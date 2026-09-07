package infrastructure

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/eagle-go/eagle/internal/auth/domain"
	"github.com/eagle-go/eagle/pkg/authn"
)

func TestIssuedTokenPassesRuntimeVerifier(t *testing.T) {
	const secret = "test-signing-secret-at-least-32-bytes"
	issuer, err := NewTokenIssuer(secret, "https://eagle.test", "eagle-api", "eagle-api", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	raw, err := issuer.Issue(&domain.Identity{
		Subject: "google:123", Email: "user@example.com", DisplayName: "User", Role: "user",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	verifier := authn.NewVerifier(context.Background(), authn.Config{
		Issuer: "https://eagle.test", Audience: "eagle-api", ClientID: "eagle-api", SigningSecret: secret,
		Claims: authn.ClaimPaths{RealmRoles: "realm_access.roles", ClientRoles: "resource_access"},
	})
	claims, err := verifier.Verify(context.Background(), raw)
	if err != nil {
		t.Fatalf("verify issued token: %v", err)
	}
	if claims.Subject != "google:123" || !slices.Equal(claims.Roles(authn.ClaimPaths{RealmRoles: "realm_access.roles", ClientRoles: "resource_access"}, "eagle-api"), []string{"realm:user"}) {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestValidNonce(t *testing.T) {
	const nonce = "random-nonce-with-enough-entropy"
	if !validNonce(domain.ProviderGoogle, nonce, nonce) {
		t.Fatal("Google nonce should match verbatim")
	}
	if validNonce(domain.ProviderGoogle, "wrong", nonce) {
		t.Fatal("Google nonce must reject mismatch")
	}
	if !validNonce(domain.ProviderApple, "70f77163935e7484d9e94f9f2a377e802b2c250bda65ed1d5aea4c11ca382b48", nonce) {
		t.Fatal("Apple nonce should accept SHA-256 hex")
	}
}

func TestProviderVerifierChecksSignatureAudienceAndNonce(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const keyID = "test-key"
	jwks, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key: key.Public(), KeyID: keyID, Algorithm: string(jose.RS256), Use: "sig",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(string(jwks))),
		}, nil
	})}
	ctx := oidc.ClientContext(context.Background(), client)

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", keyID),
	)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	raw, err := jwt.Signed(signer).Claims(jwt.Claims{
		Issuer: "https://provider.test", Subject: "provider-subject", Audience: jwt.Audience{"google-client"},
		IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(now.Add(time.Minute)),
	}).Claims(map[string]any{
		"nonce": "nonce-with-128-bits-minimum", "email": "user@example.com", "email_verified": true,
	}).Serialize()
	if err != nil {
		t.Fatal(err)
	}

	verifier := newProviderVerifier(ctx, providerSettings{
		enabled: true, clientID: "google-client", issuer: "https://provider.test", jwks: "https://provider.test/keys",
	}, providerSettings{})
	identity, err := verifier.Verify(context.Background(), domain.ProviderGoogle, raw, "nonce-with-128-bits-minimum")
	if err != nil {
		t.Fatalf("verify provider token: %v", err)
	}
	if identity.ProviderID != "provider-subject" || !identity.EmailVerified {
		t.Fatalf("unexpected identity: %+v", identity)
	}
	if _, err := verifier.Verify(context.Background(), domain.ProviderGoogle, raw, "different-nonce-with-128-bits"); !errors.Is(err, domain.ErrInvalidNonce) {
		t.Fatalf("nonce mismatch error = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
