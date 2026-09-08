package interfaces

import (
	"context"
	"errors"
	"testing"
	"time"

	v1 "github.com/eagle-go/eagle/api/eagle/auth/v1"
	"github.com/eagle-go/eagle/internal/auth/application"
	"github.com/eagle-go/eagle/internal/auth/domain"
)

type providerStub struct {
	provider domain.Provider
	err      error
}

func (p *providerStub) Verify(_ context.Context, provider domain.Provider, _, _ string) (*domain.ExternalIdentity, error) {
	p.provider = provider
	if p.err != nil {
		return nil, p.err
	}
	return &domain.ExternalIdentity{Provider: provider, ProviderID: "external-user"}, nil
}

type sessionStub struct {
	domain.SessionRepository
	identity   *domain.Identity
	createHash string
	rotateHash string
	revokeHash string
	err        error
}

func (s *sessionStub) grant() (*domain.SessionGrant, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &domain.SessionGrant{Identity: s.identity, AccessToken: "access-token"}, nil
}
func (s *sessionStub) Create(_ context.Context, _ *domain.ExternalIdentity, session domain.Session) (*domain.SessionGrant, error) {
	s.createHash = session.RefreshTokenHash
	return s.grant()
}
func (s *sessionStub) Rotate(_ context.Context, oldHash, _ string, _ time.Time) (*domain.SessionGrant, error) {
	s.rotateHash = oldHash
	return s.grant()
}
func (s *sessionStub) Revoke(_ context.Context, hash string) error {
	s.revokeHash = hash
	return s.err
}

func newAuthService(provider *providerStub, sessions *sessionStub) *AuthService {
	return NewAuthService(application.NewUsecase(provider, sessions, 15*time.Minute, 30*24*time.Hour))
}

func TestSocialLoginMapsProvidersAndTokens(t *testing.T) {
	for _, tc := range []struct {
		name           string
		protoProvider  v1.SocialProvider
		domainProvider domain.Provider
	}{
		{"google", v1.SocialProvider_SOCIAL_PROVIDER_GOOGLE, domain.ProviderGoogle},
		{"apple", v1.SocialProvider_SOCIAL_PROVIDER_APPLE, domain.ProviderApple},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &providerStub{}
			sessions := &sessionStub{identity: &domain.Identity{
				Subject: "user-1", Provider: tc.domainProvider, Email: "user@example.com",
				EmailVerified: true, DisplayName: "Eagle", AvatarURL: "https://example.com/avatar", Role: "user",
			}}
			got, err := newAuthService(provider, sessions).SocialLogin(context.Background(), &v1.SocialLoginRequest{
				Provider: tc.protoProvider, IdToken: "id-token", Nonce: "nonce", DisplayName: "Request Name",
			})
			if err != nil {
				t.Fatal(err)
			}
			if provider.provider != tc.domainProvider || got.GetAccessToken() != "access-token" ||
				got.GetRefreshToken() == "" || got.GetTokenType() != "Bearer" || got.GetExpiresIn() != 900 ||
				got.GetUser().GetProvider() != tc.protoProvider || got.GetUser().GetSubject() != "user-1" ||
				got.GetUser().GetEmail() != "user@example.com" || !got.GetUser().GetEmailVerified() ||
				got.GetUser().GetDisplayName() != "Eagle" || got.GetUser().GetAvatarUrl() == "" ||
				len(got.GetUser().GetRoles()) != 1 || got.GetUser().GetRoles()[0] != "user" || sessions.createHash == "" {
				t.Fatalf("response = %+v, provider=%q, hash=%q", got, provider.provider, sessions.createHash)
			}
		})
	}
}

func TestSocialLoginRejectsUnsupportedProvider(t *testing.T) {
	provider := &providerStub{}
	sessions := &sessionStub{}
	got, err := newAuthService(provider, sessions).SocialLogin(context.Background(), &v1.SocialLoginRequest{})
	if got != nil || !errors.Is(err, domain.ErrProviderDisabled) || provider.provider != "" {
		t.Fatalf("login = %+v, %v; provider=%q", got, err, provider.provider)
	}
}

func TestAuthServiceRefreshAndLogout(t *testing.T) {
	provider := &providerStub{}
	sessions := &sessionStub{identity: &domain.Identity{Subject: "user-1", Provider: domain.ProviderGoogle, Role: "user"}}
	service := newAuthService(provider, sessions)
	refreshed, err := service.RefreshToken(context.Background(), &v1.RefreshTokenRequest{RefreshToken: "old-refresh"})
	if err != nil || refreshed.GetAccessToken() != "access-token" || refreshed.GetRefreshToken() == "" || sessions.rotateHash == "" {
		t.Fatalf("refresh = %+v, hash=%q, err=%v", refreshed, sessions.rotateHash, err)
	}
	loggedOut, err := service.Logout(context.Background(), &v1.LogoutRequest{RefreshToken: "old-refresh"})
	if err != nil || loggedOut == nil || sessions.revokeHash != sessions.rotateHash {
		t.Fatalf("logout = %+v, hash=%q, err=%v", loggedOut, sessions.revokeHash, err)
	}
}

func TestAuthServicePropagatesUsecaseErrors(t *testing.T) {
	want := errors.New("session unavailable")
	provider := &providerStub{err: want}
	sessions := &sessionStub{err: want}
	service := newAuthService(provider, sessions)
	if got, err := service.SocialLogin(context.Background(), &v1.SocialLoginRequest{Provider: v1.SocialProvider_SOCIAL_PROVIDER_GOOGLE}); got != nil || !errors.Is(err, want) {
		t.Fatalf("login = %+v, %v", got, err)
	}
	if got, err := service.RefreshToken(context.Background(), &v1.RefreshTokenRequest{RefreshToken: "old"}); got != nil || !errors.Is(err, want) {
		t.Fatalf("refresh = %+v, %v", got, err)
	}
	if got, err := service.Logout(context.Background(), &v1.LogoutRequest{RefreshToken: "old"}); got != nil || !errors.Is(err, want) {
		t.Fatalf("logout = %+v, %v", got, err)
	}
}
