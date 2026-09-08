package e2e

import (
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

type tokenOpts struct {
	subject   string
	roles     []string
	audience  []string
	expiresIn time.Duration
	notBefore time.Duration
	wrongKey  bool
	algorithm jose.SignatureAlgorithm
}

func mintEagleToken(t *testing.T, opts tokenOpts) string {
	t.Helper()
	alg := opts.algorithm
	if alg == "" {
		alg = jose.ES256
	}
	var key any = testSigningKey
	if opts.wrongKey {
		key = testWrongKey
	}
	if alg == jose.HS512 {
		key = []byte(strings.Repeat("x", 64))
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: alg, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", testKeyID),
	)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	expiresIn := opts.expiresIn
	if expiresIn == 0 {
		expiresIn = time.Hour
	}
	audience := opts.audience
	if audience == nil {
		audience = []string{testAudience}
	}
	standard := jwt.Claims{
		Issuer: testIssuer, Subject: opts.subject, Audience: audience, ID: "test-token-id",
		IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(now.Add(expiresIn)),
	}
	if opts.notBefore != 0 {
		standard.NotBefore = jwt.NewNumericDate(now.Add(opts.notBefore))
	}
	raw, err := jwt.Signed(signer).Claims(standard).Claims(map[string]any{
		"sid": "test-session-id", "roles": opts.roles,
	}).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func userToken(t *testing.T, label string, roles ...string) string {
	t.Helper()
	return mintEagleToken(t, tokenOpts{
		subject: "subject-" + label, roles: roles,
	})
}
