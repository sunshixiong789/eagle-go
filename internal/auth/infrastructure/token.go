package infrastructure

import (
	"fmt"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/eagle-go/eagle/internal/auth/domain"
)

type TokenIssuer struct {
	signer   jose.Signer
	issuer   string
	audience string
	ttl      time.Duration
}

func NewTokenIssuer(secret, issuer, audience string, ttl time.Duration) (domain.AccessTokenIssuer, error) {
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.HS256, Key: []byte(secret)},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	if err != nil {
		return nil, fmt.Errorf("create access token signer: %w", err)
	}
	return &TokenIssuer{signer: signer, issuer: issuer, audience: audience, ttl: ttl}, nil
}

func (i *TokenIssuer) Issue(identity *domain.Identity, now time.Time) (string, error) {
	claims := jwt.Claims{
		Issuer: i.issuer, Subject: identity.Subject, Audience: jwt.Audience{i.audience},
		IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(now.Add(i.ttl)),
	}
	private := map[string]any{
		"preferred_username": identity.DisplayName,
		"email":              identity.Email,
		"roles":              []string{identity.Role},
	}
	token, err := jwt.Signed(i.signer).Claims(claims).Claims(private).Serialize()
	if err != nil {
		return "", fmt.Errorf("issue access token: %w", err)
	}
	return token, nil
}
