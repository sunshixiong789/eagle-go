package authn

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	kratoserrors "github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/transport"

	"github.com/eagle-go/eagle/pkg/identity"
)

const (
	testIssuer   = "https://eagle.test"
	testAudience = "eagle-api"
	testKeyID    = "current"
)

type tokenTestKeys struct {
	signer jose.Signer
	set    *StaticKeySet
}

func newTokenTestKeys(t *testing.T) tokenTestKeys {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: private},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", testKeyID),
	)
	if err != nil {
		t.Fatal(err)
	}
	return tokenTestKeys{signer: signer, set: NewStaticKeySet(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key: &private.PublicKey, KeyID: testKeyID, Algorithm: string(jose.ES256), Use: "sig",
	}}})}
}

func newTestVerifier(t *testing.T, keys KeySource) *Verifier {
	t.Helper()
	verifier, err := NewVerifier(Config{Issuer: testIssuer, Audience: testAudience, Keys: keys})
	if err != nil {
		t.Fatal(err)
	}
	return verifier
}

func TestVerifierAcceptsMinimalEagleClaims(t *testing.T) {
	keys := newTokenTestKeys(t)
	raw := signTestToken(t, keys.signer, jwt.Claims{
		Issuer: testIssuer, Subject: "account-subject", Audience: jwt.Audience{testAudience}, ID: "token-id",
		IssuedAt: jwt.NewNumericDate(time.Now()), Expiry: jwt.NewNumericDate(time.Now().Add(time.Minute)),
	}, map[string]any{
		"sid": "session-id", "roles": []string{"viewer", "editor"},
		// Resource services must not turn profile PII into Principal fields.
		"email": "ignored@example.com", "preferred_username": "ignored",
	})

	claims, err := newTestVerifier(t, keys.set).Verify(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "account-subject" || claims.SessionID != "session-id" || claims.TokenID != "token-id" ||
		!slices.Equal(claims.Roles, []string{"viewer", "editor"}) {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestVerifierRejectsInvalidEagleTokens(t *testing.T) {
	keys := newTokenTestKeys(t)
	other := newTokenTestKeys(t)
	now := time.Now()
	validClaims := func() jwt.Claims {
		return jwt.Claims{
			Issuer: testIssuer, Subject: "account", Audience: jwt.Audience{testAudience}, ID: "token-id",
			IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(now.Add(time.Minute)),
		}
	}
	tests := []struct {
		name string
		raw  func(*testing.T) string
		is   error
	}{
		{
			name: "expired",
			raw: func(t *testing.T) string {
				claims := validClaims()
				claims.IssuedAt, claims.Expiry = jwt.NewNumericDate(now.Add(-2*time.Hour)), jwt.NewNumericDate(now.Add(-time.Hour))
				return signTestToken(t, keys.signer, claims, map[string]any{"sid": "session"})
			},
			is: ErrTokenExpired,
		},
		{
			name: "wrong signature",
			raw: func(t *testing.T) string {
				return signTestToken(t, other.signer, validClaims(), map[string]any{"sid": "session"})
			},
			is: ErrInvalidToken,
		},
		{
			name: "wrong audience",
			raw: func(t *testing.T) string {
				claims := validClaims()
				claims.Audience = jwt.Audience{"another-api"}
				return signTestToken(t, keys.signer, claims, map[string]any{"sid": "session"})
			},
			is: ErrInvalidToken,
		},
		{
			name: "missing required sid",
			raw:  func(t *testing.T) string { return signTestToken(t, keys.signer, validClaims(), nil) },
			is:   ErrInvalidToken,
		},
		{name: "malformed", raw: func(*testing.T) string { return "not-a-jwt" }, is: ErrInvalidToken},
	}

	verifier := newTestVerifier(t, keys.set)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := verifier.Verify(context.Background(), tt.raw(t))
			if !errors.Is(err, tt.is) {
				t.Fatalf("error = %v, want errors.Is(%v)", err, tt.is)
			}
		})
	}
}

func signTestToken(t *testing.T, signer jose.Signer, standard jwt.Claims, private map[string]any) string {
	t.Helper()
	raw, err := jwt.Signed(signer).Claims(standard).Claims(private).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

type headerCarrier http.Header

func (h headerCarrier) Get(key string) string      { return http.Header(h).Get(key) }
func (h headerCarrier) Set(key, value string)      { http.Header(h).Set(key, value) }
func (h headerCarrier) Add(key, value string)      { http.Header(h).Add(key, value) }
func (h headerCarrier) Keys() []string             { return sortedHeaderKeys(h) }
func (h headerCarrier) Values(key string) []string { return http.Header(h).Values(key) }

func sortedHeaderKeys(h headerCarrier) []string {
	keys := make([]string, 0, len(h))
	for key := range h {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

type authnTransport struct{ header headerCarrier }

func (t *authnTransport) Kind() transport.Kind            { return transport.KindHTTP }
func (t *authnTransport) Endpoint() string                { return "" }
func (t *authnTransport) Operation() string               { return "/test.Service/Call" }
func (t *authnTransport) RequestHeader() transport.Header { return t.header }
func (t *authnTransport) ReplyHeader() transport.Header   { return t.header }

func authnServerContext(authorization string) context.Context {
	header := headerCarrier{}
	if authorization != "" {
		header.Set("Authorization", authorization)
	}
	return transport.NewServerContext(context.Background(), &authnTransport{header: header})
}

func TestServerAuthenticationBoundaries(t *testing.T) {
	keys := newTokenTestKeys(t)
	verifier := newTestVerifier(t, keys.set)
	valid := signTestToken(t, keys.signer, jwt.Claims{
		Issuer: testIssuer, Subject: "user-1", Audience: jwt.Audience{testAudience}, ID: "token-id",
		IssuedAt: jwt.NewNumericDate(time.Now()), Expiry: jwt.NewNumericDate(time.Now().Add(time.Minute)),
	}, map[string]any{"sid": "session-1", "roles": []string{"viewer"}})

	for _, tc := range []struct {
		name   string
		ctx    context.Context
		called bool
		code   int
		reason string
	}{
		{name: "no transport", ctx: context.Background(), called: true},
		{name: "no header", ctx: authnServerContext(""), called: true},
		{name: "wrong scheme", ctx: authnServerContext("Basic abc"), called: true},
		{name: "empty bearer", ctx: authnServerContext("Bearer   "), called: true},
		{name: "malformed token", ctx: authnServerContext("Bearer broken"), code: 401, reason: ReasonUnauthenticated},
		{name: "valid lowercase scheme", ctx: authnServerContext("bearer " + valid), called: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			response, err := Server(verifier)(func(ctx context.Context, _ any) (any, error) {
				called = true
				if tc.name == "valid lowercase scheme" {
					principal, ok := identity.FromContext(ctx)
					if !ok || principal.Subject != "user-1" || principal.SessionID != "session-1" ||
						!slices.Equal(principal.Roles, []string{"viewer"}) {
						t.Fatalf("principal = %+v, %v", principal, ok)
					}
				}
				return "ok", nil
			})(tc.ctx, nil)
			if called != tc.called {
				t.Fatalf("called = %v, want %v", called, tc.called)
			}
			if tc.code == 0 {
				if err != nil || response != "ok" {
					t.Fatalf("response = %v, error = %v", response, err)
				}
				return
			}
			if response != nil || kratoserrors.Code(err) != tc.code || kratoserrors.Reason(err) != tc.reason {
				t.Fatalf("response = %v, error = %v", response, err)
			}
		})
	}
}

func TestServerReportsExpiredTokenSeparately(t *testing.T) {
	keys := newTokenTestKeys(t)
	expired := signTestToken(t, keys.signer, jwt.Claims{
		Issuer: testIssuer, Subject: "user-1", Audience: jwt.Audience{testAudience}, ID: "token-id",
		IssuedAt: jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)), Expiry: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
	}, map[string]any{"sid": "session-1"})
	called := false
	_, err := Server(newTestVerifier(t, keys.set))(
		func(context.Context, any) (any, error) { called = true; return nil, nil },
	)(authnServerContext("Bearer "+expired), nil)
	if called || kratoserrors.Code(err) != 401 || kratoserrors.Reason(err) != ReasonTokenExpired {
		t.Fatalf("called=%v, error=%v", called, err)
	}
}

func TestNewVerifierRejectsIncompleteConfig(t *testing.T) {
	if _, err := NewVerifier(Config{}); err == nil {
		t.Fatal("incomplete verifier config accepted")
	}
}
