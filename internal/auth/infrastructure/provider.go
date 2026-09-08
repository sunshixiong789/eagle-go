package infrastructure

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/eagle-go/eagle/internal/auth/domain"
	"github.com/eagle-go/eagle/pkg/platform/config"
)

const (
	googleIssuer = "https://accounts.google.com"
	googleJWKS   = "https://www.googleapis.com/oauth2/v3/certs"
	appleIssuer  = "https://appleid.apple.com"
	appleJWKS    = "https://appleid.apple.com/auth/keys"
)

type ProviderVerifier struct {
	google *oidc.IDTokenVerifier
	apple  *oidc.IDTokenVerifier
}

func NewProviderVerifier(c *config.Auth) domain.ProviderVerifier {
	return newProviderVerifier(context.Background(),
		providerSettings{enabled: c.GetGoogle().GetEnabled(), clientID: c.GetGoogle().GetClientId(), issuer: googleIssuer, jwks: googleJWKS},
		providerSettings{enabled: c.GetApple().GetEnabled(), clientID: c.GetApple().GetClientId(), issuer: appleIssuer, jwks: appleJWKS},
	)
}

type providerSettings struct {
	enabled  bool
	clientID string
	issuer   string
	jwks     string
}

func newProviderVerifier(ctx context.Context, googleSettings, appleSettings providerSettings) *ProviderVerifier {
	var google, apple *oidc.IDTokenVerifier
	if googleSettings.enabled {
		google = oidc.NewVerifier(googleSettings.issuer, oidc.NewRemoteKeySet(ctx, googleSettings.jwks), &oidc.Config{
			ClientID: googleSettings.clientID, SupportedSigningAlgs: []string{"RS256"},
		})
	}
	if appleSettings.enabled {
		apple = oidc.NewVerifier(appleSettings.issuer, oidc.NewRemoteKeySet(ctx, appleSettings.jwks), &oidc.Config{
			ClientID: appleSettings.clientID, SupportedSigningAlgs: []string{"RS256"},
		})
	}
	return &ProviderVerifier{google: google, apple: apple}
}

func (v *ProviderVerifier) Verify(ctx context.Context, provider domain.Provider, rawToken, nonce string) (*domain.ExternalIdentity, error) {
	verifier := v.google
	if provider == domain.ProviderApple {
		verifier = v.apple
	} else if provider != domain.ProviderGoogle {
		return nil, domain.ErrProviderDisabled
	}
	if verifier == nil {
		return nil, domain.ErrProviderDisabled
	}
	token, err := verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("%w: provider verification: %w", domain.ErrInvalidIDToken, err)
	}
	if !validNonce(provider, token.Nonce, nonce) {
		return nil, domain.ErrInvalidNonce
	}
	var claims providerClaims
	if err := token.Claims(&claims); err != nil || claims.Subject == "" {
		return nil, domain.ErrInvalidIDToken
	}
	return &domain.ExternalIdentity{
		Provider: provider, ProviderID: claims.Subject, Email: claims.Email,
		EmailVerified: bool(claims.EmailVerified), DisplayName: claims.Name, AvatarURL: claims.Picture,
	}, nil
}

// validNonce 要求凭证与请求的 nonce 一致，并兼容 Apple 返回原始 nonce 的 SHA-256 十六进制值。
func validNonce(provider domain.Provider, tokenNonce, rawNonce string) bool {
	if tokenNonce == "" || rawNonce == "" {
		return false
	}
	if tokenNonce == rawNonce {
		return true
	}
	if provider != domain.ProviderApple {
		return false
	}
	sum := sha256.Sum256([]byte(rawNonce))
	return tokenNonce == hex.EncodeToString(sum[:])
}

type providerClaims struct {
	Subject       string       `json:"sub"`
	Email         string       `json:"email"`
	EmailVerified flexibleBool `json:"email_verified"`
	Name          string       `json:"name"`
	Picture       string       `json:"picture"`
}

type flexibleBool bool

func (b *flexibleBool) UnmarshalJSON(data []byte) error {
	var value bool
	if err := json.Unmarshal(data, &value); err == nil {
		*b = flexibleBool(value)
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return errors.New("email_verified must be bool or string")
	}
	*b = flexibleBool(text == "true")
	return nil
}
