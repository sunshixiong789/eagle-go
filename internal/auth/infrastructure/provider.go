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
	"github.com/eagle-go/eagle/internal/platform/config"
)

const (
	googleIssuer = "https://accounts.google.com"
	googleJWKS   = "https://www.googleapis.com/oauth2/v3/certs"
	appleIssuer  = "https://appleid.apple.com"
	appleJWKS    = "https://appleid.apple.com/auth/keys"
)

type ProviderVerifier struct {
	google    *oidc.IDTokenVerifier
	googleIDs []string
	apple     *oidc.IDTokenVerifier
	appleIDs  []string
}

func NewProviderVerifier(c *config.Auth) domain.ProviderVerifier {
	return newProviderVerifier(context.Background(),
		providerSettings{enabled: c.GetGoogle().GetEnabled(), clientIDs: config.ClientIDs(c.GetGoogle().GetClientId()), issuer: googleIssuer, jwks: googleJWKS},
		providerSettings{enabled: c.GetApple().GetEnabled(), clientIDs: config.ClientIDs(c.GetApple().GetClientId()), issuer: appleIssuer, jwks: appleJWKS},
	)
}

type providerSettings struct {
	enabled   bool
	clientIDs []string
	issuer    string
	jwks      string
}

func newProviderVerifier(ctx context.Context, googleSettings, appleSettings providerSettings) *ProviderVerifier {
	return &ProviderVerifier{
		google:    newIDTokenVerifier(ctx, googleSettings),
		googleIDs: googleSettings.clientIDs,
		apple:     newIDTokenVerifier(ctx, appleSettings),
		appleIDs:  appleSettings.clientIDs,
	}
}

func newIDTokenVerifier(ctx context.Context, settings providerSettings) *oidc.IDTokenVerifier {
	if !settings.enabled || len(settings.clientIDs) == 0 {
		return nil
	}
	// audience 由调用方按允许列表核对，以便一个提供商接受多个 Client ID。
	return oidc.NewVerifier(settings.issuer, oidc.NewRemoteKeySet(ctx, settings.jwks), &oidc.Config{
		SkipClientIDCheck: true, SupportedSigningAlgs: []string{"RS256"},
	})
}

func (v *ProviderVerifier) Verify(ctx context.Context, provider domain.Provider, rawToken, nonce string) (*domain.ExternalIdentity, error) {
	verifier, clientIDs := v.google, v.googleIDs
	if provider == domain.ProviderApple {
		verifier, clientIDs = v.apple, v.appleIDs
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
	if !audienceAllowed(token.Audience, clientIDs) {
		return nil, fmt.Errorf("%w: audience", domain.ErrInvalidIDToken)
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
		TokenExpiresAt: token.Expiry,
	}, nil
}

func audienceAllowed(got, allowed []string) bool {
	for _, aud := range got {
		for _, want := range allowed {
			if aud == want {
				return true
			}
		}
	}
	return false
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
