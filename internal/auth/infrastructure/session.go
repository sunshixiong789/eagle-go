package infrastructure

import (
	"context"
	"fmt"
	"time"

	"github.com/eagle-go/eagle/internal/auth/domain"
	platformdb "github.com/eagle-go/eagle/internal/platform/database"
	"github.com/eagle-go/eagle/internal/platform/database/ent"
	"github.com/eagle-go/eagle/internal/platform/database/ent/authsession"
	"github.com/eagle-go/eagle/internal/platform/database/ent/socialidentity"
)

type SessionRepository struct {
	db     *platformdb.Database
	issuer domain.AccessTokenIssuer
}

func NewSessionRepository(db *platformdb.Database, issuer domain.AccessTokenIssuer) domain.SessionRepository {
	return &SessionRepository{db: db, issuer: issuer}
}

func (r *SessionRepository) Create(ctx context.Context, external *domain.ExternalIdentity, session domain.Session) (*domain.SessionGrant, error) {
	tx, err := r.db.Client().Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin social login: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	identity, err := upsertIdentity(ctx, tx, external)
	if err != nil {
		return nil, err
	}
	if _, err := tx.AuthSession.Create().
		SetID(session.ID).
		SetIdentityID(identity.ID).
		SetRefreshTokenHash(session.RefreshTokenHash).
		SetExpiresAt(session.ExpiresAt).
		Save(ctx); err != nil {
		return nil, fmt.Errorf("create auth session: %w", err)
	}
	// 本地签发不做网络 I/O；签发失败时回滚身份写入与会话创建。
	access, err := r.issuer.Issue(identity, time.Now())
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit social login: %w", err)
	}
	return &domain.SessionGrant{Identity: identity, AccessToken: access}, nil
}

func upsertIdentity(ctx context.Context, tx *ent.Tx, external *domain.ExternalIdentity) (*domain.Identity, error) {
	now := time.Now()
	provider := string(external.Provider)
	upsert := tx.SocialIdentity.Create().
		SetSubject(provider+":"+external.ProviderID).
		SetProvider(provider).
		SetProviderSubject(external.ProviderID).
		SetEmail(external.Email).
		SetEmailVerified(external.EmailVerified).
		SetDisplayName(external.DisplayName).
		SetAvatarURL(external.AvatarURL).
		SetLastLoginAt(now).
		SetUpdatedAt(now).
		OnConflictColumns(socialidentity.FieldProvider, socialidentity.FieldProviderSubject).
		Update(func(update *ent.SocialIdentityUpsert) {
			update.UpdateEmail().UpdateEmailVerified().UpdateLastLoginAt().UpdateUpdatedAt()
			if external.DisplayName != "" {
				update.UpdateDisplayName()
			}
			if external.AvatarURL != "" {
				update.UpdateAvatarURL()
			}
		})
	if err := upsert.Exec(ctx); err != nil {
		return nil, fmt.Errorf("upsert social identity: %w", err)
	}
	row, err := tx.SocialIdentity.Query().Where(
		socialidentity.ProviderEQ(provider),
		socialidentity.ProviderSubjectEQ(external.ProviderID),
	).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("read social identity: %w", err)
	}
	return toIdentity(row), nil
}

func (r *SessionRepository) Rotate(ctx context.Context, oldHash, newHash string, expiresAt time.Time) (*domain.SessionGrant, error) {
	tx, err := r.db.Client().Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin refresh: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now()
	affected, err := tx.AuthSession.Update().Where(
		authsession.RefreshTokenHashEQ(oldHash),
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
	row, err := tx.SocialIdentity.Get(ctx, session.IdentityID)
	if err != nil {
		return nil, fmt.Errorf("read session identity: %w", err)
	}
	identity := toIdentity(row)
	access, err := r.issuer.Issue(identity, time.Now())
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit refresh: %w", err)
	}
	return &domain.SessionGrant{Identity: identity, AccessToken: access}, nil
}

func (r *SessionRepository) Revoke(ctx context.Context, hash string) error {
	now := time.Now()
	count, err := r.db.Client().AuthSession.Update().
		Where(authsession.RefreshTokenHashEQ(hash), authsession.RevokedAtIsNil()).
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

func toIdentity(row *ent.SocialIdentity) *domain.Identity {
	return &domain.Identity{
		ID:            row.ID,
		Subject:       row.Subject,
		Provider:      domain.Provider(row.Provider),
		ProviderID:    row.ProviderSubject,
		Email:         row.Email,
		EmailVerified: row.EmailVerified,
		DisplayName:   row.DisplayName,
		AvatarURL:     row.AvatarURL,
		Role:          row.Role,
	}
}
