package e2e

import (
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

type tokenOpts struct {
	subject   string
	username  string
	roles     []string
	audience  []string
	expiresIn time.Duration
	notBefore time.Duration
	wrongKey  bool
	algorithm jose.SignatureAlgorithm
}

func mintEagleToken(t *testing.T, opts tokenOpts) string {
	t.Helper()
	secret := testAuthSecret
	if opts.wrongKey {
		secret = "different-signing-secret-at-least-32-bytes"
	}
	alg := opts.algorithm
	if alg == "" {
		alg = jose.HS256
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: alg, Key: []byte(secret)},
		(&jose.SignerOptions{}).WithType("JWT"),
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
		Issuer: testIssuer, Subject: opts.subject, Audience: audience,
		IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(now.Add(expiresIn)),
	}
	if opts.notBefore != 0 {
		standard.NotBefore = jwt.NewNumericDate(now.Add(opts.notBefore))
	}
	raw, err := jwt.Signed(signer).Claims(standard).Claims(map[string]any{
		"preferred_username": opts.username,
		"roles":              opts.roles,
	}).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func userToken(t *testing.T, username string, roles ...string) string {
	t.Helper()
	return mintEagleToken(t, tokenOpts{
		subject: "subject-" + username, username: username, roles: roles,
	})
}
