package infrastructure

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
	keyDirectory := testTokenKeyDirectory(t, "current")
	issuer, err := NewTokenIssuer(
		keyDirectory, "current",
		"https://eagle.test", "eagle-api", 15*time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	if keys := issuer.PublicKeySet().Keys; len(keys) != 1 || keys[0].KeyID != "current" || !keys[0].IsPublic() {
		t.Fatalf("public key set = %+v", keys)
	}
	now := time.Now()
	raw, err := issuer.Issue(&domain.Identity{
		Subject: "account-subject", Email: "user@example.com", DisplayName: "User", Roles: []string{"user"},
	}, "session-id", now)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := authn.NewVerifier(authn.Config{
		Issuer: "https://eagle.test", Audience: "eagle-api", Keys: authn.NewStaticKeySet(issuer.PublicKeySet()),
	})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := verifier.Verify(context.Background(), raw)
	if err != nil {
		t.Fatalf("verify issued token: %v", err)
	}
	if claims.Subject != "account-subject" || claims.SessionID != "session-id" || claims.TokenID == "" ||
		!slices.Equal(claims.Roles, []string{"user"}) {
		t.Fatalf("unexpected claims: %+v", claims)
	}
	parsed, err := jwt.ParseSigned(raw, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := parsed.UnsafeClaimsWithoutVerification(&payload); err != nil {
		t.Fatal(err)
	}
	if payload["email"] != nil || payload["preferred_username"] != nil {
		t.Fatalf("token leaked profile PII: %v", payload)
	}
}

func TestTokenIssuerRejectsMissingKeyMaterial(t *testing.T) {
	if _, err := NewTokenIssuer(filepath.Join(t.TempDir(), "missing"), "current", "https://eagle.test", "eagle-api", time.Minute); err == nil {
		t.Fatal("missing key directory accepted")
	}
	if _, err := NewTokenIssuer(t.TempDir(), "current", "https://eagle.test", "eagle-api", time.Minute); err == nil {
		t.Fatal("missing active key accepted")
	}
}

func testTokenKeyDirectory(t *testing.T, kid string) string {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, kid+".pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
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
