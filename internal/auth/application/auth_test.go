package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eagle-go/eagle/internal/auth/domain"
)

type providerFake struct{ err error }

func (p providerFake) Verify(context.Context, domain.Provider, string, string) (*domain.ExternalIdentity, error) {
	if p.err != nil {
		return nil, p.err
	}
	return &domain.ExternalIdentity{Provider: domain.ProviderGoogle, ProviderID: "user"}, nil
}

type sessionsFake struct {
	domain.SessionRepository
	create func(context.Context, *domain.ExternalIdentity, string, domain.Session) (*domain.SessionGrant, error)
	rotate func(context.Context, string, string, time.Time) (*domain.SessionGrant, error)
	revoke func(context.Context, string) error
}

func (s sessionsFake) Create(ctx context.Context, e *domain.ExternalIdentity, subject string, session domain.Session) (*domain.SessionGrant, error) {
	return s.create(ctx, e, subject, session)
}
func (s sessionsFake) Rotate(ctx context.Context, old, next string, expiry time.Time) (*domain.SessionGrant, error) {
	return s.rotate(ctx, old, next, expiry)
}
func (s sessionsFake) Revoke(ctx context.Context, hash string) error {
	return s.revoke(ctx, hash)
}

func TestLoginDoesNotPersistUnverifiedIdentity(t *testing.T) {
	want := errors.New("provider failed")
	uc := NewUsecase(providerFake{want}, sessionsFake{}, time.Minute, time.Hour)
	if tokens, err := uc.Login(context.Background(), domain.ProviderGoogle, "id", "nonce", ""); tokens != nil || !errors.Is(err, want) {
		t.Fatalf("login = %+v, %v", tokens, err)
	}
}

func TestRefreshReturnsOnlyCommittedGrant(t *testing.T) {
	now := time.Now()
	failure := errors.New("transaction failed")
	for _, commitErr := range []error{nil, failure} {
		t.Run(map[bool]string{true: "failure", false: "success"}[commitErr != nil], func(t *testing.T) {
			var hash string
			uc := NewUsecase(providerFake{}, sessionsFake{rotate: func(_ context.Context, old, next string, expiry time.Time) (*domain.SessionGrant, error) {
				if old != tokenHash("old-token") || next == old || !expiry.Equal(now.Add(time.Hour)) {
					t.Fatal("incorrect rotation arguments")
				}
				hash = next
				if commitErr != nil {
					return nil, commitErr
				}
				return &domain.SessionGrant{AccessToken: "access", Identity: &domain.Identity{Subject: "user"}}, nil
			}}, time.Minute, time.Hour)
			uc.now = func() time.Time { return now }
			tokens, err := uc.Refresh(context.Background(), "old-token")
			if commitErr != nil {
				if tokens != nil || !errors.Is(err, commitErr) {
					t.Fatalf("refresh = %+v, %v", tokens, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tokenHash(tokens.RefreshToken) != hash || tokens.AccessToken != "access" || tokens.ExpiresIn != time.Minute {
				t.Fatalf("tokens = %+v", tokens)
			}
		})
	}
}

func TestLoginPersistsOnlyRefreshHash(t *testing.T) {
	now := time.Now()
	var saved domain.Session
	uc := NewUsecase(providerFake{}, sessionsFake{create: func(_ context.Context, e *domain.ExternalIdentity, subject string, s domain.Session) (*domain.SessionGrant, error) {
		saved = s
		if e.DisplayName != "Name" || e.ProviderID != "user" || len(subject) != 32 {
			t.Fatalf("external = %+v, subject = %q", e, subject)
		}
		return &domain.SessionGrant{Identity: &domain.Identity{Subject: "user"}, AccessToken: "access"}, nil
	}}, time.Minute, time.Hour)
	uc.now = func() time.Time { return now }
	tokens, err := uc.Login(context.Background(), domain.ProviderGoogle, "id", "nonce", "Name")
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.ID) != 32 || saved.RefreshTokenHash != tokenHash(tokens.RefreshToken) || !saved.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("session = %+v", saved)
	}
}

func TestLoginPreservesVerifiedDisplayName(t *testing.T) {
	provider := providerFakeWithIdentity{identity: &domain.ExternalIdentity{
		Provider: domain.ProviderApple, ProviderID: "apple-user", DisplayName: "Verified Name",
	}}
	uc := NewUsecase(provider, sessionsFake{create: func(_ context.Context, e *domain.ExternalIdentity, _ string, _ domain.Session) (*domain.SessionGrant, error) {
		if e.DisplayName != "Verified Name" {
			t.Fatalf("display name = %q", e.DisplayName)
		}
		return &domain.SessionGrant{Identity: &domain.Identity{}, AccessToken: "access"}, nil
	}}, time.Minute, time.Hour)
	if _, err := uc.Login(context.Background(), domain.ProviderApple, "id", "nonce", "Request Name"); err != nil {
		t.Fatal(err)
	}
}

type providerFakeWithIdentity struct {
	identity *domain.ExternalIdentity
}

func (p providerFakeWithIdentity) Verify(context.Context, domain.Provider, string, string) (*domain.ExternalIdentity, error) {
	return p.identity, nil
}

func TestLoginPropagatesCreateFailure(t *testing.T) {
	want := errors.New("create failed")
	uc := NewUsecase(providerFake{}, sessionsFake{create: func(context.Context, *domain.ExternalIdentity, string, domain.Session) (*domain.SessionGrant, error) {
		return nil, want
	}}, time.Minute, time.Hour)
	tokens, err := uc.Login(context.Background(), domain.ProviderGoogle, "id", "nonce", "")
	if tokens != nil || !errors.Is(err, want) {
		t.Fatalf("login = %+v, %v", tokens, err)
	}
}

func TestLogoutHashesRefreshToken(t *testing.T) {
	want := errors.New("revoke failed")
	uc := NewUsecase(providerFake{}, sessionsFake{revoke: func(_ context.Context, hash string) error {
		if hash != tokenHash("refresh-token") {
			t.Fatalf("hash = %q", hash)
		}
		return want
	}}, time.Minute, time.Hour)
	if err := uc.Logout(context.Background(), "refresh-token"); !errors.Is(err, want) {
		t.Fatalf("logout error = %v", err)
	}
}
