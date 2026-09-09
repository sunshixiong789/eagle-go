package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eagle-go/eagle/internal/auth/domain"
	"github.com/eagle-go/eagle/internal/platform/database/ent/authsession"
)

func TestSessionLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	repo := NewSessionRepository(authTestDB, &testIssuer{}, "eagle-api")
	identity, err := repo.Create(context.Background(), &domain.ExternalIdentity{
		Provider: domain.ProviderGoogle, ProviderID: "provider-user-1", Email: "user@example.com",
		EmailVerified: true, DisplayName: "User",
	}, "11111111111111111111111111111111", domain.Session{ID: "session0000000000000000000000001", RefreshTokenHash: "old-hash", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if identity.Identity.Subject != "11111111111111111111111111111111" || len(identity.Identity.Roles) != 1 || identity.Identity.Roles[0] != "user" {
		t.Fatalf("unexpected identity: %+v", identity)
	}
	updated, err := repo.Create(context.Background(), &domain.ExternalIdentity{
		Provider: domain.ProviderGoogle, ProviderID: "provider-user-1", Email: "updated@example.com",
		EmailVerified: true,
	}, "22222222222222222222222222222222", domain.Session{ID: "session0000000000000000000000002", RefreshTokenHash: "other-session", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Identity.ID != identity.Identity.ID || updated.Identity.Email != "updated@example.com" || updated.Identity.DisplayName != "User" {
		t.Fatalf("upserted identity = %+v", updated.Identity)
	}

	rotated, err := repo.Rotate(context.Background(), "old-hash", "new-hash", time.Now().Add(2*time.Hour))
	if err != nil || rotated.Identity.ID != identity.Identity.ID {
		t.Fatalf("rotate = %+v, %v", rotated, err)
	}
	if _, err := repo.Rotate(context.Background(), "old-hash", "other-hash", time.Now().Add(time.Hour)); !errors.Is(err, domain.ErrInvalidRefreshToken) {
		t.Fatalf("reusing old token error = %v", err)
	}
	if err := repo.Revoke(context.Background(), "new-hash"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Rotate(context.Background(), "new-hash", "third-hash", time.Now().Add(time.Hour)); !errors.Is(err, domain.ErrInvalidRefreshToken) {
		t.Fatalf("revoked token error = %v", err)
	}
}

func TestSessionPreservesUnicodeProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	ctx := context.Background()
	repo := NewSessionRepository(authTestDB, &testIssuer{}, "eagle-api")
	t.Cleanup(func() { _ = authTestDB.Client().UserAccount.DeleteOneID("unicode-account-0").Exec(ctx) })
	for i, character := range []string{"汉", "😀"} {
		external := &domain.ExternalIdentity{
			Provider: domain.ProviderGoogle, ProviderID: "unicode-profile-user",
			DisplayName: strings.Repeat(character, 128),
			AvatarURL:   "https://example.com/" + strings.Repeat(character, 2000),
			Email:       strings.Repeat(character, 300) + "@example.com",
		}
		grant, err := repo.Create(ctx, external, fmt.Sprintf("unicode-account-%d", i), domain.Session{
			ID: fmt.Sprintf("unicode-session-%d", i), RefreshTokenHash: fmt.Sprintf("unicode-refresh-%d", i),
			ExpiresAt: time.Now().Add(time.Hour),
		})
		if err != nil {
			t.Fatalf("save Unicode profile: %v", err)
		}
		if grant.Identity.Subject != "unicode-account-0" || grant.Identity.DisplayName != external.DisplayName ||
			grant.Identity.AvatarURL != external.AvatarURL || grant.Identity.Email != external.Email {
			t.Fatal("Unicode profile was not preserved during account creation/update")
		}
	}
}

func TestProviderSubjectsRemainCaseSensitive(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	repo := NewSessionRepository(authTestDB, &testIssuer{}, "eagle-api")
	var identities []*domain.Identity
	for i, providerID := range []string{"CaseSensitive", "casesensitive"} {
		grant, err := repo.Create(context.Background(), &domain.ExternalIdentity{
			Provider: domain.ProviderGoogle, ProviderID: providerID,
		}, fmt.Sprintf("%032d", i+10), domain.Session{
			ID: fmt.Sprintf("case-sensitive-session-%08d", i), RefreshTokenHash: fmt.Sprintf("case-sensitive-hash-%d", i),
			ExpiresAt: time.Now().Add(time.Hour),
		})
		if err != nil {
			t.Fatal(err)
		}
		identities = append(identities, grant.Identity)
	}
	if identities[0].ID == identities[1].ID {
		t.Fatalf("case-distinct provider subjects collapsed to identity %d", identities[0].ID)
	}
}

func TestRolesAreScopedByAudience(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	ctx := context.Background()
	consumerRepo := NewSessionRepository(authTestDB, &testIssuer{}, "consumer-api")
	external := &domain.ExternalIdentity{Provider: domain.ProviderGoogle, ProviderID: "audience-scoped-user"}
	consumer, err := consumerRepo.Create(ctx, external, "55555555555555555555555555555555", domain.Session{
		ID: "audience-consumer-session-0001", RefreshTokenHash: "audience-consumer-old", ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(consumer.Identity.Roles) != 1 || consumer.Identity.Roles[0] != "user" {
		t.Fatalf("consumer roles = %v", consumer.Identity.Roles)
	}
	if _, err := authTestDB.Client().UserRoleBinding.Create().
		SetAccountSubject(consumer.Identity.Subject).
		SetAudience("backoffice-api").
		SetRole("admin").
		Save(ctx); err != nil {
		t.Fatal(err)
	}

	backofficeRepo := NewSessionRepository(authTestDB, &testIssuer{}, "backoffice-api")
	backoffice, err := backofficeRepo.Create(ctx, external, "66666666666666666666666666666666", domain.Session{
		ID: "audience-backoffice-session-01", RefreshTokenHash: "audience-backoffice-old", ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(backoffice.Identity.Roles) != 1 || backoffice.Identity.Roles[0] != "admin" {
		t.Fatalf("backoffice roles = %v", backoffice.Identity.Roles)
	}
	storedSession, err := authTestDB.Client().AuthSession.Query().Where(
		authsession.RefreshTokenHashEQ("audience-backoffice-old"),
	).Only(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if storedSession.Audience != "backoffice-api" {
		t.Fatalf("session audience = %q", storedSession.Audience)
	}
}

func TestDisabledAccountCannotRefreshAndRotationRollsBack(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	ctx := context.Background()
	repo := NewSessionRepository(authTestDB, &testIssuer{}, "eagle-api")
	grant, err := repo.Create(ctx,
		&domain.ExternalIdentity{Provider: domain.ProviderGoogle, ProviderID: "disabled-account"},
		"77777777777777777777777777777777",
		domain.Session{ID: "disabled-account-session-000001", RefreshTokenHash: "disabled-account-old", ExpiresAt: time.Now().Add(time.Hour)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authTestDB.Client().UserAccount.UpdateOneID(grant.Identity.Subject).SetStatus(0).Save(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Rotate(ctx, "disabled-account-old", "disabled-account-rejected", time.Now().Add(time.Hour)); !errors.Is(err, domain.ErrAccountDisabled) {
		t.Fatalf("disabled account refresh error = %v", err)
	}
	if _, err := authTestDB.Client().UserAccount.UpdateOneID(grant.Identity.Subject).SetStatus(1).Save(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Rotate(ctx, "disabled-account-old", "disabled-account-new", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("old token must survive disabled-account rollback: %v", err)
	}
}

type testIssuer struct{ err error }

func (i *testIssuer) Issue(_ *domain.Identity, _ string, _ time.Time) (string, error) {
	if i.err != nil {
		return "", i.err
	}
	return "signed-access", nil
}

func TestSigningFailureRollsBackSession(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	ctx := context.Background()
	signingErr := errors.New("signing unavailable")
	issuer := &testIssuer{err: signingErr}
	repo := NewSessionRepository(authTestDB, issuer, "eagle-api")
	external := &domain.ExternalIdentity{Provider: domain.ProviderGoogle, ProviderID: "signing-failure"}
	session := domain.Session{ID: "signing-failure", RefreshTokenHash: "signing-old", ExpiresAt: time.Now().Add(time.Hour)}
	if _, err := repo.Create(ctx, external, "33333333333333333333333333333333", session); !errors.Is(err, signingErr) {
		t.Fatalf("create = %v", err)
	}
	var count int
	if err := authTestDB.SQL().QueryRowContext(ctx, `SELECT count(*) FROM user_identity WHERE provider_subject = 'signing-failure'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("failed signing committed identity")
	}
	issuer.err = nil
	if _, err := repo.Create(ctx, external, "33333333333333333333333333333333", session); err != nil {
		t.Fatal(err)
	}
	issuer.err = signingErr
	if _, err := repo.Rotate(ctx, "signing-old", "signing-failed", time.Now().Add(time.Hour)); !errors.Is(err, signingErr) {
		t.Fatalf("rotate = %v", err)
	}
	issuer.err = nil
	if _, err := repo.Rotate(ctx, "signing-failed", "unexpected", time.Now().Add(time.Hour)); !errors.Is(err, domain.ErrInvalidRefreshToken) {
		t.Fatalf("failed rotation left new token usable: %v", err)
	}
	grant, err := repo.Rotate(ctx, "signing-old", "signing-retried", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("old token must survive signing failure: %v", err)
	}
	if grant.AccessToken != "signed-access" {
		t.Fatalf("grant = %+v", grant)
	}
}

func TestConcurrentRefreshHasOneWinner(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	ctx := context.Background()
	repo := NewSessionRepository(authTestDB, &testIssuer{}, "eagle-api")
	_, err := repo.Create(ctx, &domain.ExternalIdentity{Provider: domain.ProviderGoogle, ProviderID: "concurrent-refresh"}, "44444444444444444444444444444444", domain.Session{ID: "concurrent-refresh", RefreshTokenHash: "concurrent-old", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, hash := range []string{"concurrent-a", "concurrent-b"} {
		go func() {
			<-start
			_, err := repo.Rotate(ctx, "concurrent-old", hash, time.Now().Add(time.Hour))
			results <- err
		}()
	}
	close(start)
	successes := 0
	for range 2 {
		err := <-results
		if err == nil {
			successes++
		} else if !errors.Is(err, domain.ErrInvalidRefreshToken) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful rotations = %d", successes)
	}
}
