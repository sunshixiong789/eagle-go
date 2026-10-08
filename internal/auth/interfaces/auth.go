package interfaces

import (
	"context"
	"encoding/json"

	jose "github.com/go-jose/go-jose/v4"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/eagle-go/eagle/api/eagle/auth/v1"
	"github.com/eagle-go/eagle/internal/auth/application"
	"github.com/eagle-go/eagle/internal/auth/domain"
	"github.com/eagle-go/eagle/pkg/identity"
)

// AuthService 提供 Eagle 登录、令牌刷新、退出和验签公钥的 HTTP 入口。
type AuthService struct {
	uc   *application.Usecase
	keys publicKeySetProvider
}

// publicKeySetProvider 提供可公开发布的 Eagle 验签密钥集合。
type publicKeySetProvider interface {
	// PublicKeySet 返回仅含公钥的 JWKS，不得包含私钥材料。
	PublicKeySet() jose.JSONWebKeySet
}

func NewAuthService(uc *application.Usecase, keys publicKeySetProvider) *AuthService {
	return &AuthService{uc: uc, keys: keys}
}

// GetJSONWebKeySet 返回 Eagle access token 的验签公钥。
func (s *AuthService) GetJSONWebKeySet(context.Context, *v1.GetJSONWebKeySetRequest) (*v1.GetJSONWebKeySetResponse, error) {
	out := &v1.GetJSONWebKeySetResponse{}
	for _, key := range s.keys.PublicKeySet().Keys {
		raw, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		var fields struct {
			Kty string `json:"kty"`
			Use string `json:"use"`
			Alg string `json:"alg"`
			Kid string `json:"kid"`
			Crv string `json:"crv"`
			X   string `json:"x"`
			Y   string `json:"y"`
			N   string `json:"n"`
			E   string `json:"e"`
		}
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, err
		}
		out.Keys = append(out.Keys, &v1.JSONWebKey{
			Kty: fields.Kty, Use: fields.Use, Alg: fields.Alg, Kid: fields.Kid,
			Crv: fields.Crv, X: fields.X, Y: fields.Y, N: fields.N, E: fields.E,
		})
	}
	return out, nil
}

// SocialLogin 将第三方登录请求交给认证用例，返回 Eagle 令牌和账号资料。
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

// RefreshToken 轮换会话的刷新凭证并返回新的 Eagle 令牌。
func (s *AuthService) RefreshToken(ctx context.Context, req *v1.RefreshTokenRequest) (*v1.TokenResponse, error) {
	tokens, err := s.uc.Refresh(ctx, req.GetRefreshToken())
	if err != nil {
		return nil, err
	}
	return toTokenResponse(tokens), nil
}

// Logout 撤销刷新凭证对应的会话。
func (s *AuthService) Logout(ctx context.Context, req *v1.LogoutRequest) (*v1.LogoutResponse, error) {
	if err := s.uc.Logout(ctx, req.GetRefreshToken()); err != nil {
		return nil, err
	}
	return &v1.LogoutResponse{}, nil
}

// ListMySessions 返回当前账号仍可刷新的会话，并标出当前请求使用的会话。
func (s *AuthService) ListMySessions(ctx context.Context, _ *v1.ListMySessionsRequest) (*v1.ListMySessionsResponse, error) {
	principal, ok := identity.FromContext(ctx)
	if !ok {
		return nil, domain.ErrSessionInactive
	}
	rows, err := s.uc.ListMySessions(ctx, principal.Subject)
	if err != nil {
		return nil, err
	}
	out := &v1.ListMySessionsResponse{}
	for _, row := range rows {
		out.Sessions = append(out.Sessions, &v1.SessionInfo{
			SessionId: row.ID,
			CreatedAt: timestamppb.New(row.CreatedAt),
			ExpiresAt: timestamppb.New(row.ExpiresAt),
			Current:   row.ID == principal.SessionID,
		})
	}
	return out, nil
}

// RevokeMySession 撤销当前账号的另一条会话。
func (s *AuthService) RevokeMySession(ctx context.Context, req *v1.RevokeMySessionRequest) (*v1.RevokeMySessionResponse, error) {
	principal, ok := identity.FromContext(ctx)
	if !ok {
		return nil, domain.ErrSessionInactive
	}
	if err := s.uc.RevokeMySession(ctx, principal.Subject, req.GetSessionId(), principal.SessionID); err != nil {
		return nil, err
	}
	return &v1.RevokeMySessionResponse{}, nil
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
			AvatarUrl: identity.AvatarURL, Roles: identity.Roles,
		},
	}
}
