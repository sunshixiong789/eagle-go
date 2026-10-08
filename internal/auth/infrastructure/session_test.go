package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eagle-go/eagle/internal/auth/domain"
	"github.com/eagle-go/eagle/internal/platform/database/ent/authsession"
	"github.com/eagle-go/eagle/internal/platform/database/ent/authusedcredential"
)

var credentialSeq atomic.Uint64

// assignCredential 为一次登录分配独立凭证摘要。签发失败后的重试必须复用同一次分配，才能证明事务回滚。
func assignCredential(identity *domain.ExternalIdentity) *domain.ExternalIdentity {
	n := credentialSeq.Add(1)
	identity.CredentialHash = fmt.Sprintf("%064x", n)
	identity.TokenExpiresAt = time.Now().Add(time.Hour)
	return identity
}

func TestSessionLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	repo := NewSessionManager(authTestDB, &testIssuer{}, "eagle-api")
	identity, err := repo.Create(context.Background(), assignCredential(&domain.ExternalIdentity{
		Provider: domain.ProviderGoogle, ProviderID: "provider-user-1", Email: "user@example.com",
		EmailVerified: true, DisplayName: "User",
	}), "11111111111111111111111111111111", domain.Session{ID: "session0000000000000000000000001", RefreshTokenHash: "old-hash", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if identity.Identity.Subject != "11111111111111111111111111111111" || len(identity.Identity.Roles) != 1 || identity.Identity.Roles[0] != "user" {
		t.Fatalf("unexpected identity: %+v", identity)
	}
	updated, err := repo.Create(context.Background(), assignCredential(&domain.ExternalIdentity{
		Provider: domain.ProviderGoogle, ProviderID: "provider-user-1", Email: "updated@example.com",
		EmailVerified: true,
	}), "22222222222222222222222222222222", domain.Session{ID: "session0000000000000000000000002", RefreshTokenHash: "other-session", ExpiresAt: time.Now().Add(time.Hour)})
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
	repo := NewSessionManager(authTestDB, &testIssuer{}, "eagle-api")
	t.Cleanup(func() { _ = authTestDB.Client().UserAccount.DeleteOneID("unicode-account-0").Exec(ctx) })
	var firstName string
	for i, character := range []string{"汉", "😀"} {
		external := assignCredential(&domain.ExternalIdentity{
			Provider: domain.ProviderGoogle, ProviderID: "unicode-profile-user",
			DisplayName: strings.Repeat(character, 128),
			AvatarURL:   "https://example.com/" + strings.Repeat(character, 2000),
			Email:       strings.Repeat(character, 300) + "@example.com",
		})
		grant, err := repo.Create(ctx, external, fmt.Sprintf("unicode-account-%d", i), domain.Session{
			ID: fmt.Sprintf("unicode-session-%d", i), RefreshTokenHash: fmt.Sprintf("unicode-refresh-%d", i),
			ExpiresAt: time.Now().Add(time.Hour),
		})
		if err != nil {
			t.Fatalf("save Unicode profile: %v", err)
		}
		if i == 0 {
			firstName = external.DisplayName
		}
		if grant.Identity.Subject != "unicode-account-0" || grant.Identity.DisplayName != firstName ||
			grant.Identity.AvatarURL != external.AvatarURL || grant.Identity.Email != external.Email {
			t.Fatalf("profile = %+v, want name %q", grant.Identity, firstName)
		}
	}
}

func TestProviderSubjectsRemainCaseSensitive(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	repo := NewSessionManager(authTestDB, &testIssuer{}, "eagle-api")
	var identities []*domain.Identity
	for i, providerID := range []string{"CaseSensitive", "casesensitive"} {
		grant, err := repo.Create(context.Background(), assignCredential(&domain.ExternalIdentity{
			Provider: domain.ProviderGoogle, ProviderID: providerID,
		}), fmt.Sprintf("%032d", i+10), domain.Session{
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
	consumerRepo := NewSessionManager(authTestDB, &testIssuer{}, "consumer-api")
	external := assignCredential(&domain.ExternalIdentity{Provider: domain.ProviderGoogle, ProviderID: "audience-scoped-user"})
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

	backofficeRepo := NewSessionManager(authTestDB, &testIssuer{}, "backoffice-api")
	assignCredential(external)
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
	repo := NewSessionManager(authTestDB, &testIssuer{}, "eagle-api")
	grant, err := repo.Create(ctx,
		assignCredential(&domain.ExternalIdentity{Provider: domain.ProviderGoogle, ProviderID: "disabled-account"}),
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
	repo := NewSessionManager(authTestDB, issuer, "eagle-api")
	external := assignCredential(&domain.ExternalIdentity{Provider: domain.ProviderGoogle, ProviderID: "signing-failure"})
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
	used, err := authTestDB.Client().AuthUsedCredential.Query().Where(authusedcredential.IDEQ(external.CredentialHash)).Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if used != 0 {
		t.Fatal("failed signing committed credential")
	}
	issuer.err = nil
	if _, err := repo.Create(ctx, external, "33333333333333333333333333333333", session); err != nil {
		t.Fatal(err)
	}
	if used, err = authTestDB.Client().AuthUsedCredential.Query().Where(authusedcredential.IDEQ(external.CredentialHash)).Count(ctx); err != nil || used != 1 {
		t.Fatalf("credential rows = %d, %v", used, err)
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
	repo := NewSessionManager(authTestDB, &testIssuer{}, "eagle-api")
	_, err := repo.Create(ctx, assignCredential(&domain.ExternalIdentity{Provider: domain.ProviderGoogle, ProviderID: "concurrent-refresh"}), "44444444444444444444444444444444", domain.Session{ID: "concurrent-refresh", RefreshTokenHash: "concurrent-old", ExpiresAt: time.Now().Add(time.Hour)})
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

func TestCredentialIsSingleUseAndSessionAccessFollowsAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	ctx := context.Background()
	repo := NewSessionManager(authTestDB, &testIssuer{}, "access-audience")
	expiredHash := fmt.Sprintf("%064x", credentialSeq.Add(1))
	if _, err := authTestDB.Client().AuthUsedCredential.Create().
		SetID(expiredHash).
		SetExpiresAt(time.Now().Add(-time.Minute)).
		Save(ctx); err != nil {
		t.Fatal(err)
	}
	missing := &domain.ExternalIdentity{
		Provider: domain.ProviderGoogle, ProviderID: "access-missing-hash", TokenExpiresAt: time.Now().Add(time.Hour),
	}
	if _, err := repo.Create(ctx, missing, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", domain.Session{
		ID: "access-missing-hash-session01", RefreshTokenHash: "access-missing-hash", ExpiresAt: time.Now().Add(time.Hour),
	}); !errors.Is(err, domain.ErrInvalidIDToken) {
		t.Fatalf("empty credential = %v", err)
	}
	stale := assignCredential(&domain.ExternalIdentity{Provider: domain.ProviderGoogle, ProviderID: "access-stale-token"})
	stale.TokenExpiresAt = time.Now().Add(-time.Second)
	if _, err := repo.Create(ctx, stale, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", domain.Session{
		ID: "access-stale-token-session01", RefreshTokenHash: "access-stale-token", ExpiresAt: time.Now().Add(time.Hour),
	}); !errors.Is(err, domain.ErrInvalidIDToken) {
		t.Fatalf("expired credential = %v", err)
	}

	external := assignCredential(&domain.ExternalIdentity{
		Provider: domain.ProviderGoogle, ProviderID: "access-active-user", DisplayName: "First",
	})
	const firstID = "access-active-session-0000001"
	grant, err := repo.Create(ctx, external, "cccccccccccccccccccccccccccccccc", domain.Session{
		ID: firstID, RefreshTokenHash: "access-active-refresh-1", ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := authTestDB.Client().AuthUsedCredential.Query().Where(authusedcredential.IDEQ(expiredHash)).Count(ctx); err != nil || n != 0 {
		t.Fatalf("expired credential rows = %d, %v", n, err)
	}
	if _, err := repo.Create(ctx, external, "dddddddddddddddddddddddddddddddd", domain.Session{
		ID: "access-replay-session-0000001", RefreshTokenHash: "access-replay-refresh", ExpiresAt: time.Now().Add(time.Hour),
	}); !errors.Is(err, domain.ErrCredentialUsed) {
		t.Fatalf("replay = %v", err)
	}
	if err := repo.AccessActive(ctx, grant.Identity.Subject, firstID); err != nil {
		t.Fatal(err)
	}
	if err := repo.AccessActive(ctx, "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", firstID); !errors.Is(err, domain.ErrSessionInactive) {
		t.Fatalf("foreign subject = %v", err)
	}
	otherAudience := NewSessionManager(authTestDB, &testIssuer{}, "other-access-audience")
	if err := otherAudience.AccessActive(ctx, grant.Identity.Subject, firstID); !errors.Is(err, domain.ErrSessionInactive) {
		t.Fatalf("other audience = %v", err)
	}
	if listed, err := otherAudience.ListActive(ctx, grant.Identity.Subject); err != nil || len(listed) != 0 {
		t.Fatalf("other audience sessions = %+v, %v", listed, err)
	}

	external.DisplayName = "Changed"
	assignCredential(external)
	const secondID = "access-active-session-0000002"
	second, err := repo.Create(ctx, external, "ffffffffffffffffffffffffffffffff", domain.Session{
		ID: secondID, RefreshTokenHash: "access-active-refresh-2", ExpiresAt: time.Now().Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Identity.DisplayName != "First" {
		t.Fatalf("display name = %q", second.Identity.DisplayName)
	}
	rows, err := repo.ListActive(ctx, grant.Identity.Subject)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].CreatedAt.Before(rows[1].CreatedAt) {
		t.Fatalf("sessions = %+v", rows)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		seen[row.ID] = true
	}
	if !seen[firstID] || !seen[secondID] {
		t.Fatalf("sessions = %+v", rows)
	}
	if err := repo.RevokeID(ctx, grant.Identity.Subject, firstID); err != nil {
		t.Fatal(err)
	}
	if err := repo.AccessActive(ctx, grant.Identity.Subject, firstID); !errors.Is(err, domain.ErrSessionInactive) {
		t.Fatalf("revoked session = %v", err)
	}
	if err := repo.RevokeID(ctx, grant.Identity.Subject, firstID); !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("revoke again = %v", err)
	}
	if err := repo.RevokeID(ctx, "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", secondID); !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("foreign revoke = %v", err)
	}
	if _, err := authTestDB.Client().AuthSession.UpdateOneID(secondID).SetExpiresAt(time.Now().Add(-time.Minute)).Save(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.AccessActive(ctx, grant.Identity.Subject, secondID); !errors.Is(err, domain.ErrSessionInactive) {
		t.Fatalf("expired session = %v", err)
	}
	if _, err := authTestDB.Client().AuthSession.UpdateOneID(secondID).SetExpiresAt(time.Now().Add(time.Hour)).Save(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := authTestDB.Client().UserAccount.UpdateOneID(grant.Identity.Subject).SetStatus(0).Save(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.AccessActive(ctx, grant.Identity.Subject, secondID); !errors.Is(err, domain.ErrAccountDisabled) {
		t.Fatalf("disabled account = %v", err)
	}
}
