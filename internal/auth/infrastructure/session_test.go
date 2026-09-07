package infrastructure

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eagle-go/eagle/internal/auth/domain"
)

func TestSessionLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	repo := NewSessionRepository(authTestDB, &testIssuer{})
	identity, err := repo.Create(context.Background(), &domain.ExternalIdentity{
		Provider: domain.ProviderGoogle, ProviderID: "provider-user-1", Email: "user@example.com",
		EmailVerified: true, DisplayName: "User",
	}, domain.Session{ID: "session0000000000000000000000001", RefreshTokenHash: "old-hash", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if identity.Identity.Subject != "google:provider-user-1" || identity.Identity.Role != "user" {
		t.Fatalf("unexpected identity: %+v", identity)
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

type testIssuer struct{ err error }

func (i *testIssuer) Issue(_ *domain.Identity, _ time.Time) (string, error) {
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
	repo := NewSessionRepository(authTestDB, issuer)
	external := &domain.ExternalIdentity{Provider: domain.ProviderGoogle, ProviderID: "signing-failure"}
	session := domain.Session{ID: "signing-failure", RefreshTokenHash: "signing-old", ExpiresAt: time.Now().Add(time.Hour)}
	if _, err := repo.Create(ctx, external, session); !errors.Is(err, signingErr) {
		t.Fatalf("create = %v", err)
	}
	var count int
	if err := authTestDB.SQL().QueryRowContext(ctx, `SELECT count(*) FROM social_identity WHERE provider_subject = 'signing-failure'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("failed signing committed identity")
	}
	issuer.err = nil
	if _, err := repo.Create(ctx, external, session); err != nil {
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
	repo := NewSessionRepository(authTestDB, &testIssuer{})
	_, err := repo.Create(ctx, &domain.ExternalIdentity{Provider: domain.ProviderGoogle, ProviderID: "concurrent-refresh"}, domain.Session{ID: "concurrent-refresh", RefreshTokenHash: "concurrent-old", ExpiresAt: time.Now().Add(time.Hour)})
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
