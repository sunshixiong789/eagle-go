package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	platformdb "github.com/eagle-go/eagle/internal/platform/database"
	"github.com/eagle-go/eagle/internal/platform/database/ent/authsession"
	"github.com/eagle-go/eagle/internal/platform/database/ent/useridentity"
)

type tokenOpts struct {
	subject   string
	sessionID string
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
	sessionID := opts.sessionID
	if sessionID == "" {
		sessionID = "test-session-id"
	}
	raw, err := jwt.Signed(signer).Claims(standard).Claims(map[string]any{
		"sid": sessionID, "roles": opts.roles,
	}).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// userToken 签发一张已挂上真实会话的 access token，受保护接口会核对会话仍有效。
func (e *testEnv) userToken(t *testing.T, label string, roles ...string) string {
	t.Helper()
	subject := "subject-" + label
	if len(subject) > 32 {
		t.Fatalf("subject 超过 32 字符: %s", subject)
	}
	sum := sha256.Sum256([]byte(subject))
	sessionID := hex.EncodeToString(sum[:16])
	ctx := context.Background()
	if _, err := e.db.Client().UserAccount.Create().SetID(subject).Save(ctx); err != nil && !platformdb.IsUniqueViolation(err) {
		t.Fatal(err)
	}
	identity, err := e.db.Client().UserIdentity.Query().Where(
		useridentity.ProviderEQ("test"), useridentity.ProviderSubjectEQ(subject),
	).Only(ctx)
	if platformdb.IsNotFound(err) {
		identity, err = e.db.Client().UserIdentity.Create().
			SetAccountSubject(subject).SetProvider("test").SetProviderSubject(subject).
			SetLastLoginAt(time.Now()).Save(ctx)
	}
	if err != nil {
		t.Fatal(err)
	}
	exists, err := e.db.Client().AuthSession.Query().Where(authsession.IDEQ(sessionID)).Exist(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		refreshHash := sha256.Sum256([]byte("refresh-" + sessionID))
		if _, err := e.db.Client().AuthSession.Create().
			SetID(sessionID).SetIdentityID(identity.ID).SetAudience(testAudience).
			SetRefreshTokenHash(hex.EncodeToString(refreshHash[:])).
			SetExpiresAt(time.Now().Add(time.Hour)).Save(ctx); err != nil {
			t.Fatal(err)
		}
	}
	return mintEagleToken(t, tokenOpts{subject: subject, sessionID: sessionID, roles: roles})
}
