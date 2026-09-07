package service

import (
	"context"

	v1 "github.com/eagle-go/eagle/api/eagle/auth/v1"
	"github.com/eagle-go/eagle/internal/auth/application"
	"github.com/eagle-go/eagle/internal/auth/domain"
)

type AuthService struct {
	uc *application.Usecase
}

func NewAuthService(uc *application.Usecase) *AuthService { return &AuthService{uc: uc} }

func (s *AuthService) SocialLogin(ctx context.Context, req *v1.SocialLoginRequest) (*v1.TokenResponse, error) {
	var provider domain.Provider
	switch req.GetProvider() {
	case v1.SocialProvider_SOCIAL_PROVIDER_GOOGLE:
		provider = domain.ProviderGoogle
	case v1.SocialProvider_SOCIAL_PROVIDER_APPLE:
		provider = domain.ProviderApple
	default:
		return nil, domain.ErrProviderDisabled
	}
	tokens, err := s.uc.Login(ctx, provider, req.GetIdToken(), req.GetNonce(), req.GetDisplayName())
	if err != nil {
		return nil, err
	}
	return toTokenResponse(tokens), nil
}

func (s *AuthService) RefreshToken(ctx context.Context, req *v1.RefreshTokenRequest) (*v1.TokenResponse, error) {
	tokens, err := s.uc.Refresh(ctx, req.GetRefreshToken())
	if err != nil {
		return nil, err
	}
	return toTokenResponse(tokens), nil
}

func (s *AuthService) Logout(ctx context.Context, req *v1.LogoutRequest) (*v1.LogoutResponse, error) {
	if err := s.uc.Logout(ctx, req.GetRefreshToken()); err != nil {
		return nil, err
	}
	return &v1.LogoutResponse{}, nil
}

func toTokenResponse(tokens *domain.Tokens) *v1.TokenResponse {
	identity := tokens.Identity
	provider := v1.SocialProvider_SOCIAL_PROVIDER_GOOGLE
	if identity.Provider == domain.ProviderApple {
		provider = v1.SocialProvider_SOCIAL_PROVIDER_APPLE
	}
	return &v1.TokenResponse{
		AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken,
		TokenType: "Bearer", ExpiresIn: int64(tokens.ExpiresIn.Seconds()),
		User: &v1.User{
			Subject: identity.Subject, Provider: provider, Email: identity.Email,
			EmailVerified: identity.EmailVerified, DisplayName: identity.DisplayName,
			AvatarUrl: identity.AvatarURL, Roles: []string{identity.Role},
		},
	}
}
