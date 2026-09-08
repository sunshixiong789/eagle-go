package authn

import (
	"context"
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
	verifier := NewVerifier(Config{Issuer: testIssuer, Audience: testAudience, SigningSecret: testSecret})
	valid := signTestToken(t, testSecret, jwt.Claims{
		Issuer: testIssuer, Subject: "user-1", Audience: jwt.Audience{testAudience},
		IssuedAt: jwt.NewNumericDate(time.Now()), Expiry: jwt.NewNumericDate(time.Now().Add(time.Minute)),
	}, map[string]any{"preferred_username": "Eagle", "email": "user@example.com", "roles": []string{"viewer"}})

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
					if !ok || principal.Subject != "user-1" || principal.Username != "Eagle" ||
						principal.Email != "user@example.com" || !slices.Equal(principal.Roles, []string{"viewer"}) {
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
	expired := signTestToken(t, testSecret, jwt.Claims{
		Issuer: testIssuer, Subject: "user-1", Audience: jwt.Audience{testAudience},
		IssuedAt: jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)), Expiry: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
	}, nil)
	called := false
	_, err := Server(NewVerifier(Config{Issuer: testIssuer, Audience: testAudience, SigningSecret: testSecret}))(
		func(context.Context, any) (any, error) { called = true; return nil, nil },
	)(authnServerContext("Bearer "+expired), nil)
	if called || kratoserrors.Code(err) != 401 || kratoserrors.Reason(err) != ReasonTokenExpired {
		t.Fatalf("called=%v, error=%v", called, err)
	}
}
