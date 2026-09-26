package infrastructure

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"time"

	"github.com/eagle-go/eagle/internal/auth/domain"
	platformdb "github.com/eagle-go/eagle/internal/platform/database"
	"github.com/eagle-go/eagle/internal/platform/database/ent"
	"github.com/eagle-go/eagle/internal/platform/database/ent/authsession"
	"github.com/eagle-go/eagle/internal/platform/database/ent/useraudience"
	"github.com/eagle-go/eagle/internal/platform/database/ent/useridentity"
)

const accountStatusEnabled int32 = 1

type SessionRepository struct {
	db       *platformdb.Database
	issuer   domain.AccessTokenIssuer
	audience string
}

func NewSessionRepository(db *platformdb.Database, issuer domain.AccessTokenIssuer, audience string) domain.SessionRepository {
	return &SessionRepository{db: db, issuer: issuer, audience: audience}
}

func (r *SessionRepository) Create(
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
	update := tx.UserAccount.UpdateOneID(row.AccountSubject).SetUpdatedAt(now)
	if external.DisplayName != "" {
		update.SetDisplayName(external.DisplayName)
	}
	if external.AvatarURL != "" {
		update.SetAvatarURL(external.AvatarURL)
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
	roles := before
	if len(roles) == 0 {
		if _, err := tx.UserRoleBinding.Create().SetAccountSubject(subject).SetAudience(audience).SetRole("user").Save(ctx); err != nil {
			return nil, err
		}
		roles = []string{"user"}
	}
	if err := markAudience(ctx, tx, subject, audience); err != nil {
		return nil, err
	}
	if _, err := auditAccountRoles(ctx, tx, state, subject, subject, "audience.join", before, roles); err != nil {
		return nil, err
	}
	return roles, nil
}

func (r *SessionRepository) Rotate(ctx context.Context, oldHash, newHash string, expiresAt time.Time) (*domain.SessionGrant, error) {
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

func (r *SessionRepository) Revoke(ctx context.Context, hash string) error {
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
