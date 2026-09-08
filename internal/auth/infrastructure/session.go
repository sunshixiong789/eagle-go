package infrastructure

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/eagle-go/eagle/internal/auth/domain"
	platformdb "github.com/eagle-go/eagle/internal/platform/database"
	"github.com/eagle-go/eagle/internal/platform/database/ent"
	"github.com/eagle-go/eagle/internal/platform/database/ent/authsession"
	"github.com/eagle-go/eagle/internal/platform/database/ent/useridentity"
	"github.com/eagle-go/eagle/internal/platform/database/ent/userrolebinding"
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
	tx, err := r.db.Client().Tx(ctx)
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

	// A concurrent or returning login resolves to the authoritative account.
	// Delete the unused candidate in the same transaction so no orphan remains.
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

func ensureAndLoadRoles(ctx context.Context, tx *ent.Tx, subject, audience string) ([]string, error) {
	exists, err := tx.UserRoleBinding.Query().Where(
		userrolebinding.AccountSubjectEQ(subject),
		userrolebinding.AudienceEQ(audience),
	).Exist(ctx)
	if err != nil {
		return nil, fmt.Errorf("check user role bindings: %w", err)
	}
	if !exists {
		if err := tx.UserRoleBinding.Create().
			SetAccountSubject(subject).
			SetAudience(audience).
			SetRole("user").
			OnConflictColumns(
				userrolebinding.FieldAccountSubject,
				userrolebinding.FieldAudience,
				userrolebinding.FieldRole,
			).
			Ignore().
			Exec(ctx); err != nil {
			return nil, fmt.Errorf("assign default user role: %w", err)
		}
	}
	rows, err := tx.UserRoleBinding.Query().Where(
		userrolebinding.AccountSubjectEQ(subject),
		userrolebinding.AudienceEQ(audience),
	).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("read user role bindings: %w", err)
	}
	roles := make([]string, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, row.Role)
	}
	slices.Sort(roles)
	return roles, nil
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
	roles, err := ensureAndLoadRoles(ctx, tx, identity.AccountSubject, r.audience)
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
