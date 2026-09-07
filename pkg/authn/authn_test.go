package authn

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

const (
	testIssuer   = "https://eagle.test"
	testAudience = "eagle-api"
	testSecret   = "test-signing-secret-at-least-32-bytes"
)

func TestVerifierAcceptsFixedEagleClaims(t *testing.T) {
	raw := signTestToken(t, testSecret, jwt.Claims{
		Issuer: testIssuer, Subject: "google:123", Audience: jwt.Audience{testAudience},
		IssuedAt: jwt.NewNumericDate(time.Now()), Expiry: jwt.NewNumericDate(time.Now().Add(time.Minute)),
	}, map[string]any{
		"preferred_username": "Eagle User",
		"email":              "user@example.com",
		"roles":              []string{"viewer", "editor"},
	})

	verifier := NewVerifier(Config{
		Issuer: testIssuer, Audience: testAudience, SigningSecret: testSecret,
	})
	claims, err := verifier.Verify(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "google:123" || claims.Username != "Eagle User" || claims.Email != "user@example.com" {
		t.Fatalf("claims = %+v", claims)
	}
	if !slices.Equal(claims.Roles, []string{"viewer", "editor"}) {
		t.Fatalf("roles = %v", claims.Roles)
	}
}

func TestVerifierRejectsInvalidEagleTokens(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		raw  func(*testing.T) string
		is   error
	}{
		{
			name: "expired",
			raw: func(t *testing.T) string {
				return signTestToken(t, testSecret, jwt.Claims{
					Issuer: testIssuer, Subject: "expired", Audience: jwt.Audience{testAudience},
					IssuedAt: jwt.NewNumericDate(now.Add(-2 * time.Hour)), Expiry: jwt.NewNumericDate(now.Add(-time.Hour)),
				}, map[string]any{"roles": []string{"user"}})
			},
			is: ErrTokenExpired,
		},
		{
			name: "wrong signature",
			raw: func(t *testing.T) string {
				return signTestToken(t, "another-signing-secret-at-least-32-bytes", jwt.Claims{
					Issuer: testIssuer, Subject: "forged", Audience: jwt.Audience{testAudience},
					IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(now.Add(time.Minute)),
				}, map[string]any{"roles": []string{"admin"}})
			},
			is: ErrInvalidToken,
		},
		{
			name: "wrong audience",
			raw: func(t *testing.T) string {
				return signTestToken(t, testSecret, jwt.Claims{
					Issuer: testIssuer, Subject: "wrong-aud", Audience: jwt.Audience{"another-api"},
					IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(now.Add(time.Minute)),
				}, map[string]any{"roles": []string{"user"}})
			},
			is: ErrInvalidToken,
		},
		{
			name: "malformed",
			raw:  func(*testing.T) string { return "not-a-jwt" },
			is:   ErrInvalidToken,
		},
	}

	verifier := NewVerifier(Config{
		Issuer: testIssuer, Audience: testAudience, SigningSecret: testSecret,
	})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := verifier.Verify(context.Background(), tt.raw(t))
			if !errors.Is(err, tt.is) {
				t.Fatalf("error = %v, want errors.Is(%v)", err, tt.is)
			}
		})
	}
}

func signTestToken(t *testing.T, secret string, standard jwt.Claims, private map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.HS256, Key: []byte(secret)},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jwt.Signed(signer).Claims(standard).Claims(private).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
