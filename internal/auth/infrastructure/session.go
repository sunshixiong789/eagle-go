package infrastructure

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/eagle-go/eagle/internal/auth/domain"
	platformdb "github.com/eagle-go/eagle/internal/platform/database"
	"github.com/eagle-go/eagle/internal/platform/database/ent"
	"github.com/eagle-go/eagle/internal/platform/database/ent/authsession"
	"github.com/eagle-go/eagle/internal/platform/database/ent/authusedcredential"
	"github.com/eagle-go/eagle/internal/platform/database/ent/useraudience"
	"github.com/eagle-go/eagle/internal/platform/database/ent/useridentity"
)

const accountStatusEnabled int32 = 1

// SessionManager 在本地签名成功后提交身份、角色与会话变更。
type SessionManager struct {
	db       *platformdb.Database
	issuer   domain.AccessTokenIssuer
	audience string
}

// NewSessionManager 构造会话原子操作的数据库适配器。
func NewSessionManager(db *platformdb.Database, issuer domain.AccessTokenIssuer, audience string) domain.SessionManager {
	return &SessionManager{db: db, issuer: issuer, audience: audience}
}

func (r *SessionManager) Create(
	ctx context.Context,
	external *domain.ExternalIdentity,
	newAccountSubject string,
	session domain.Session,
) (*domain.SessionGrant, error) {
	tx, err := r.db.Client().BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin social login: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := consumeCredential(ctx, tx, external); err != nil {
		return nil, err
	}
	identity, err := upsertIdentity(ctx, tx, external, newAccountSubject)
	if err != nil {
		return nil, err
	}
	roles, err := ensureAndLoadRoles(ctx, tx, identity.AccountSubject, r.audience)
	if err != nil {
		return nil, err
	}
	account, err := tx.UserAccount.Get(ctx, identity.AccountSubject)
	if err != nil {
		return nil, fmt.Errorf("read user account: %w", err)
	}
	if account.Status != accountStatusEnabled {
		return nil, domain.ErrAccountDisabled
	}
	principal := toIdentity(identity, account, roles)
	if _, err := tx.AuthSession.Create().
		SetID(session.ID).
		SetIdentityID(identity.ID).
		SetAudience(r.audience).
		SetRefreshTokenHash(session.RefreshTokenHash).
		SetExpiresAt(session.ExpiresAt).
		Save(ctx); err != nil {
		return nil, fmt.Errorf("create auth session: %w", err)
	}
	access, err := r.issuer.Issue(principal, session.ID, time.Now())
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit social login: %w", err)
	}
	return &domain.SessionGrant{Identity: principal, AccessToken: access}, nil
}

// upsertIdentity 通过第三方身份的唯一约束确定账号归属，并在同一事务内清理未使用的候选账号。
func upsertIdentity(
	ctx context.Context,
	tx *ent.Tx,
	external *domain.ExternalIdentity,
	newAccountSubject string,
) (*ent.UserIdentity, error) {
	now := time.Now()
	if _, err := tx.UserAccount.Create().
		SetID(newAccountSubject).
		SetDisplayName(external.DisplayName).
		SetAvatarURL(external.AvatarURL).
		Save(ctx); err != nil {
		return nil, fmt.Errorf("create candidate user account: %w", err)
	}

	provider := string(external.Provider)
	upsert := tx.UserIdentity.Create().
		SetAccountSubject(newAccountSubject).
		SetProvider(provider).
		SetProviderSubject(external.ProviderID).
		SetEmail(external.Email).
		SetEmailVerified(external.EmailVerified).
		SetLastLoginAt(now).
		SetUpdatedAt(now).
		OnConflictColumns(useridentity.FieldProvider, useridentity.FieldProviderSubject).
		Update(func(update *ent.UserIdentityUpsert) {
			update.UpdateEmail().UpdateEmailVerified().UpdateLastLoginAt().UpdateUpdatedAt()
		})
	if err := upsert.Exec(ctx); err != nil {
		return nil, fmt.Errorf("upsert user identity: %w", err)
	}
	row, err := tx.UserIdentity.Query().Where(
		useridentity.ProviderEQ(provider),
		useridentity.ProviderSubjectEQ(external.ProviderID),
	).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("read user identity: %w", err)
	}

	// 重复或并发登录可能命中已有账号，删除未使用的候选账号，避免留下孤立记录。
	if row.AccountSubject != newAccountSubject {
		if err := tx.UserAccount.DeleteOneID(newAccountSubject).Exec(ctx); err != nil {
			return nil, fmt.Errorf("delete unused candidate account: %w", err)
		}
	}
	account, err := tx.UserAccount.Get(ctx, row.AccountSubject)
	if err != nil {
		return nil, fmt.Errorf("read user account profile: %w", err)
	}
	profile := domain.MergeLoginProfile(
		domain.LoginProfile{DisplayName: account.DisplayName, AvatarURL: account.AvatarURL},
		domain.LoginProfile{DisplayName: external.DisplayName, AvatarURL: external.AvatarURL},
	)
	update := tx.UserAccount.UpdateOneID(row.AccountSubject).SetUpdatedAt(now)
	if profile.DisplayName != account.DisplayName {
		update.SetDisplayName(profile.DisplayName)
	}
	if profile.AvatarURL != account.AvatarURL {
		update.SetAvatarURL(profile.AvatarURL)
	}
	if _, err := update.Save(ctx); err != nil {
		return nil, fmt.Errorf("update user account profile: %w", err)
	}
	return row, nil
}

// ensureAndLoadRoles 仅在首次进入 audience 时补默认角色，显式空授权由初始化标记保留。
// 调用前已锁定目标账号；状态行锁与管理入口顺序一致。
func ensureAndLoadRoles(ctx context.Context, tx *ent.Tx, subject, audience string) ([]string, error) {
	initialized, err := tx.UserAudience.Query().Where(useraudience.AccountSubjectEQ(subject), useraudience.AudienceEQ(audience)).Exist(ctx)
	if err != nil {
		return nil, err
	}
	if initialized {
		return loadRoles(ctx, tx, subject, audience)
	}
	state, err := lockAccountRoleState(ctx, tx, audience)
	if err != nil {
		return nil, err
	}
	before, err := loadRoles(ctx, tx, subject, audience)
	if err != nil {
		return nil, err
	}
	roles := domain.InitialAudienceRoles(before)
	if len(before) == 0 {
		for _, role := range roles {
			if _, err := tx.UserRoleBinding.Create().SetAccountSubject(subject).SetAudience(audience).SetRole(role).Save(ctx); err != nil {
				return nil, err
			}
		}
	}
	if err := markAudience(ctx, tx, subject, audience); err != nil {
		return nil, err
	}
	if _, err := auditAccountRoles(ctx, tx, state, subject, subject, "audience.join", before, roles); err != nil {
		return nil, err
	}
	return roles, nil
}

// consumeCredential 在登录事务内记录凭证摘要。唯一冲突表示同一凭证已经换过会话。
// 事务回滚时这条记录一并撤销，签发失败后可以用原凭证重试。
func consumeCredential(ctx context.Context, tx *ent.Tx, external *domain.ExternalIdentity) error {
	now := time.Now()
	if external.CredentialHash == "" || !external.TokenExpiresAt.After(now) {
		return domain.ErrInvalidIDToken
	}
	if _, err := tx.AuthUsedCredential.Delete().Where(authusedcredential.ExpiresAtLTE(now)).Exec(ctx); err != nil {
		return fmt.Errorf("delete expired login credentials: %w", err)
	}
	if err := tx.AuthUsedCredential.Create().
		SetID(external.CredentialHash).
		SetExpiresAt(external.TokenExpiresAt).
		Exec(ctx); err != nil {
		if platformdb.IsUniqueViolation(err) {
			return domain.ErrCredentialUsed
		}
		return fmt.Errorf("store login credential: %w", err)
	}
	return nil
}

func (r *SessionManager) AccessActive(ctx context.Context, subject, sessionID string) error {
	session, err := r.db.Client().AuthSession.Query().Where(
		authsession.IDEQ(sessionID),
		authsession.AudienceEQ(r.audience),
	).Only(ctx)
	if platformdb.IsNotFound(err) {
		return domain.ErrSessionInactive
	}
	if err != nil {
		return fmt.Errorf("read access session: %w", err)
	}
	if session.RevokedAt != nil || !session.ExpiresAt.After(time.Now()) {
		return domain.ErrSessionInactive
	}
	identity, err := r.db.Client().UserIdentity.Get(ctx, session.IdentityID)
	if platformdb.IsNotFound(err) || (err == nil && identity.AccountSubject != subject) {
		return domain.ErrSessionInactive
	}
	if err != nil {
		return fmt.Errorf("read access identity: %w", err)
	}
	account, err := r.db.Client().UserAccount.Get(ctx, identity.AccountSubject)
	if platformdb.IsNotFound(err) {
		return domain.ErrSessionInactive
	}
	if err != nil {
		return fmt.Errorf("read access account: %w", err)
	}
	if account.Status != accountStatusEnabled {
		return domain.ErrAccountDisabled
	}
	return nil
}

func (r *SessionManager) ListActive(ctx context.Context, subject string) ([]domain.SessionInfo, error) {
	ids, err := r.identityIDs(ctx, subject)
	if err != nil || len(ids) == 0 {
		return []domain.SessionInfo{}, err
	}
	rows, err := r.db.Client().AuthSession.Query().Where(
		authsession.IdentityIDIn(ids...),
		authsession.AudienceEQ(r.audience),
		authsession.RevokedAtIsNil(),
		authsession.ExpiresAtGT(time.Now()),
	).Order(authsession.ByCreatedAt(entsql.OrderDesc())).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list auth sessions: %w", err)
	}
	out := make([]domain.SessionInfo, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.SessionInfo{ID: row.ID, CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt})
	}
	return out, nil
}

func (r *SessionManager) RevokeID(ctx context.Context, subject, sessionID string) error {
	ids, err := r.identityIDs(ctx, subject)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return domain.ErrSessionNotFound
	}
	now := time.Now()
	count, err := r.db.Client().AuthSession.Update().Where(
		authsession.IDEQ(sessionID),
		authsession.IdentityIDIn(ids...),
		authsession.AudienceEQ(r.audience),
		authsession.RevokedAtIsNil(),
	).SetRevokedAt(now).SetUpdatedAt(now).Save(ctx)
	if err != nil {
		return fmt.Errorf("revoke auth session: %w", err)
	}
	if count == 0 {
		return domain.ErrSessionNotFound
	}
	return nil
}

func (r *SessionManager) identityIDs(ctx context.Context, subject string) ([]int64, error) {
	rows, err := r.db.Client().UserIdentity.Query().Where(useridentity.AccountSubjectEQ(subject)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list account identities: %w", err)
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids, nil
}

func (r *SessionManager) Rotate(ctx context.Context, oldHash, newHash string, expiresAt time.Time) (*domain.SessionGrant, error) {
	tx, err := r.db.Client().BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin refresh: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now()
	// 条件更新同时检查有效性并消费旧哈希，并发请求只有一次能命中；后续签发失败由事务回滚恢复。
	affected, err := tx.AuthSession.Update().Where(
		authsession.RefreshTokenHashEQ(oldHash),
		authsession.AudienceEQ(r.audience),
		authsession.RevokedAtIsNil(),
		authsession.ExpiresAtGT(now),
	).
		SetRefreshTokenHash(newHash).
		SetExpiresAt(expiresAt).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("rotate refresh token: %w", err)
	}
	if affected == 0 {
		return nil, domain.ErrInvalidRefreshToken
	}
	session, err := tx.AuthSession.Query().Where(authsession.RefreshTokenHashEQ(newHash)).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("read rotated session: %w", err)
	}
	identity, err := tx.UserIdentity.Get(ctx, session.IdentityID)
	if err != nil {
		return nil, fmt.Errorf("read session identity: %w", err)
	}
	account, err := tx.UserAccount.Get(ctx, identity.AccountSubject)
	if err != nil {
		return nil, fmt.Errorf("read session account: %w", err)
	}
	if account.Status != accountStatusEnabled {
		return nil, domain.ErrAccountDisabled
	}
	roles, err := loadRoles(ctx, tx, identity.AccountSubject, r.audience)
	if err != nil {
		return nil, err
	}
	principal := toIdentity(identity, account, roles)
	access, err := r.issuer.Issue(principal, session.ID, time.Now())
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit refresh: %w", err)
	}
	return &domain.SessionGrant{Identity: principal, AccessToken: access}, nil
}

func (r *SessionManager) Revoke(ctx context.Context, hash string) error {
	now := time.Now()
	count, err := r.db.Client().AuthSession.Update().
		Where(
			authsession.RefreshTokenHashEQ(hash),
			authsession.AudienceEQ(r.audience),
			authsession.RevokedAtIsNil(),
		).
		SetRevokedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	if count == 0 {
		return domain.ErrInvalidRefreshToken
	}
	return nil
}

func toIdentity(row *ent.UserIdentity, account *ent.UserAccount, roles []string) *domain.Identity {
	return &domain.Identity{
		ID:            row.ID,
		Subject:       account.ID,
		Provider:      domain.Provider(row.Provider),
		ProviderID:    row.ProviderSubject,
		Email:         row.Email,
		EmailVerified: row.EmailVerified,
		DisplayName:   account.DisplayName,
		AvatarURL:     account.AvatarURL,
		Roles:         slices.Clone(roles),
	}
}
