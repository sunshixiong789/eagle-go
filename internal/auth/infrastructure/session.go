package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/eagle-go/eagle/internal/auth/domain"
	platformdb "github.com/eagle-go/eagle/internal/platform/database"
)

type SessionRepository struct {
	db     *platformdb.Database
	issuer domain.AccessTokenIssuer
}

func NewSessionRepository(db *platformdb.Database, issuer domain.AccessTokenIssuer) domain.SessionRepository {
	return &SessionRepository{db: db, issuer: issuer}
}

func (r *SessionRepository) Create(ctx context.Context, external *domain.ExternalIdentity, session domain.Session) (*domain.SessionGrant, error) {
	tx, err := r.db.SQL().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin social login: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	identity, err := upsertIdentity(ctx, tx, external)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO auth_session (id, identity_id, refresh_token_hash, expires_at)
		VALUES ($1, $2, $3, $4)`, session.ID, identity.ID, session.RefreshTokenHash, session.ExpiresAt)
	if err != nil {
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

func upsertIdentity(ctx context.Context, tx *sql.Tx, external *domain.ExternalIdentity) (*domain.Identity, error) {
	row := tx.QueryRowContext(ctx, `
		INSERT INTO social_identity
			(subject, provider, provider_subject, email, email_verified, display_name, avatar_url, last_login_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		ON CONFLICT (provider, provider_subject) DO UPDATE SET
			email = EXCLUDED.email,
			email_verified = EXCLUDED.email_verified,
			display_name = CASE WHEN EXCLUDED.display_name <> '' THEN EXCLUDED.display_name ELSE social_identity.display_name END,
			avatar_url = CASE WHEN EXCLUDED.avatar_url <> '' THEN EXCLUDED.avatar_url ELSE social_identity.avatar_url END,
			last_login_at = now(), updated_at = now()
		RETURNING id, subject, provider, provider_subject, email, email_verified, display_name, avatar_url, role`,
		string(external.Provider)+":"+external.ProviderID, external.Provider, external.ProviderID,
		external.Email, external.EmailVerified, external.DisplayName, external.AvatarURL)
	return scanIdentity(row)
}

func (r *SessionRepository) Rotate(ctx context.Context, oldHash, newHash string, expiresAt time.Time) (*domain.SessionGrant, error) {
	tx, err := r.db.SQL().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin refresh: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var identityID int64
	err = tx.QueryRowContext(ctx, `
		UPDATE auth_session SET refresh_token_hash = $2, expires_at = $3, updated_at = now()
		WHERE refresh_token_hash = $1 AND revoked_at IS NULL AND expires_at > now()
		RETURNING identity_id`, oldHash, newHash, expiresAt).Scan(&identityID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrInvalidRefreshToken
	}
	if err != nil {
		return nil, fmt.Errorf("rotate refresh token: %w", err)
	}
	identity, err := scanIdentity(tx.QueryRowContext(ctx, `
		SELECT id, subject, provider, provider_subject, email, email_verified, display_name, avatar_url, role
		FROM social_identity WHERE id = $1`, identityID))
	if err != nil {
		return nil, err
	}
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
	result, err := r.db.SQL().ExecContext(ctx, `
		UPDATE auth_session SET revoked_at = now(), updated_at = now()
		WHERE refresh_token_hash = $1 AND revoked_at IS NULL`, hash)
	if err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read revoked session count: %w", err)
	}
	if count == 0 {
		return domain.ErrInvalidRefreshToken
	}
	return nil
}

type rowScanner interface{ Scan(...any) error }

func scanIdentity(row rowScanner) (*domain.Identity, error) {
	var identity domain.Identity
	err := row.Scan(&identity.ID, &identity.Subject, &identity.Provider, &identity.ProviderID, &identity.Email,
		&identity.EmailVerified, &identity.DisplayName, &identity.AvatarURL, &identity.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrInvalidRefreshToken
	}
	if err != nil {
		return nil, fmt.Errorf("read social identity: %w", err)
	}
	return &identity, nil
}
