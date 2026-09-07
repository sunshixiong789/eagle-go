# Modular Monolith Scaffold Pruning Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the obsolete external OIDC resource-server path, legacy role model, unused compatibility code, speculative schema, and redundant dependency while preserving the runnable C-end modular monolith, Google/Apple login, RBAC, observability, and future module extraction boundaries.

**Architecture:** Keep the existing `auth`, `access`, and `dictionary` business modules with an explicit composition root. Authentication becomes one local Eagle JWT flow with fixed claims; authorization uses plain role keys and ordinary Casbin policies without a super-admin bypass. Schema and configuration are rebuilt as a clean pre-production baseline, while mature framework dependencies remain only when they have current callers.

**Tech Stack:** Go 1.27, Kratos v3 HTTP, Protobuf/buf/Protovalidate, Ent, pgx, goose, PostgreSQL 17, Casbin, go-oidc, go-jose, OpenTelemetry, Prometheus.

**Spec:** `docs/superpowers/specs/2026-09-07-modular-monolith-scaffold-pruning-design.md`

## Global Constraints

- Keep one Go module for business code, one `eagle` process, one PostgreSQL database, and one image.
- Keep Google/Apple login only; do not add phone, WeChat, account linking, or provider dependencies.
- Keep operational RBAC, policy audit, and multi-replica policy reconciliation.
- Do not add internal RPC, service discovery, queues, caches, config centers, distributed transactions, Wire, or a DI container.
- Only edit Proto, Ent schema, Go, SQL, config, deployment, and documentation sources; regenerate `*.pb.go`, OpenAPI, and `internal/platform/database/ent/**`.
- Use plain role keys such as `user` and `admin`; all authorization, including admin wildcard access, goes through Casbin.
- Preserve fail-closed behavior for unknown access policies, missing authorizers, and policy load failures.
- Preserve unrelated user changes and do not restructure files outside the accepted pruning scope.

---

### Task 1: Replace the dual resource-server verifier with fixed Eagle JWT claims

**Files:**
- Modify: `pkg/authn/authn.go`
- Modify: `pkg/authn/claims.go`
- Create: `pkg/authn/authn_test.go`
- Replace: `pkg/authn/claims_test.go`
- Modify: `pkg/platform/server/server.go`
- Modify: `internal/auth/infrastructure/token.go`
- Modify: `internal/auth/infrastructure/token_test.go`
- Modify: `tests/e2e/main_test.go`
- Modify: `tests/e2e/auth_test.go`
- Delete: `tests/e2e/oidc_provider_test.go`

**Interfaces:**
- Produces: `authn.Config{Issuer string, Audience string, SigningSecret string}`.
- Produces: `authn.Claims{Subject string, Username string, Email string, Roles []string}`.
- Produces: `authn.NewVerifier(authn.Config) *authn.Verifier`.
- Produces: `infrastructure.NewTokenIssuer(secret, issuer, audience string, ttl time.Duration) (domain.AccessTokenIssuer, error)`.
- Preserves: `authn.Server(*authn.Verifier) middleware.Middleware` and `Verifier.Verify(context.Context, string) (*Claims, error)`.

- [ ] **Step 1: Write fixed-claim verifier tests that do not configure OIDC or claim paths**

Replace dynamic claim-path expectations with direct roles and construct the verifier without a context:

```go
verifier := NewVerifier(Config{
	Issuer:        "https://eagle.test",
	Audience:      "eagle-api",
	SigningSecret: "test-signing-secret-at-least-32-bytes",
})

claims, err := verifier.Verify(context.Background(), raw)
if err != nil {
	t.Fatal(err)
}
if !slices.Equal(claims.Roles, []string{"viewer", "editor"}) {
	t.Fatalf("roles = %v", claims.Roles)
}
```

Keep table cases for expired tokens, wrong HS256 signature, invalid compact JWT, and audience mismatch. Remove RS256/JWKS resource-server cases from `pkg/authn`; provider RS256/JWKS coverage remains in `internal/auth/infrastructure/token_test.go`.

- [ ] **Step 2: Run the authn test to verify the new API fails**

Run: `GOCACHE=/tmp/eagle-go-build-cache go test ./pkg/authn -run 'Test.*Token|Test.*Claims'`

Expected: FAIL because `Config` still requires the old constructor and `Claims` has no fixed `Roles` field.

- [ ] **Step 3: Implement the minimal local JWT verifier and fixed claims**

Reduce the public types to:

```go
type Config struct {
	Issuer        string
	Audience      string
	SigningSecret string
}

type Claims struct {
	Subject  string   `json:"sub"`
	Username string   `json:"preferred_username"`
	Email    string   `json:"email"`
	Roles    []string `json:"roles"`
}
```

Delete `oidc.IDTokenVerifier`, `JWKSURL`, `JWKSPath`, `ClientID`, `ClaimPaths`, raw JSON lookup, scopes, and the remote verification branch. Keep `jwt.ParseSigned` restricted to HS256, standard claim validation, distinct expired/invalid errors, Bearer parsing, and transport error mapping.

- [ ] **Step 4: Emit the fixed roles claim from Eagle tokens**

Change the token issuer signature and payload:

```go
func NewTokenIssuer(secret, issuer, audience string, ttl time.Duration) (domain.AccessTokenIssuer, error)

private := map[string]any{
	"preferred_username": identity.DisplayName,
	"email":              identity.Email,
	"roles":              []string{identity.Role},
}
```

Update `TestIssuedTokenPassesRuntimeVerifier` to assert `claims.Roles == []string{"user"}`. Keep provider verifier signature, audience, nonce, and JWKS tests unchanged.

- [ ] **Step 5: Replace the E2E OIDC access-token issuer with an Eagle JWT helper**

Create a test-only helper in `tests/e2e/main_test.go` that signs the same fixed claims with the configured secret:

```go
func mintEagleToken(t *testing.T, secret, issuer, audience, subject string, roles []string, expiresIn time.Duration) string
```

Use HS256 and `jwt.Claims` for issuer, subject, audience, issued-at, expiry, and optional not-before. Update authentication rejection and role authorization tests to call this helper. Delete `tests/e2e/oidc_provider_test.go`; do not replace the Google/Apple provider tests in `internal/auth/infrastructure`.

Register the real `AuthService`, `SessionRepository`, and `TokenIssuer` in the E2E server with a narrow provider-port stub:

```go
type providerVerifierStub struct{}

func (providerVerifierStub) Verify(_ context.Context, provider authdomain.Provider, token, nonce string) (*authdomain.ExternalIdentity, error) {
	if provider != authdomain.ProviderGoogle || token != "valid-provider-token" || nonce != "valid-provider-nonce" {
		return nil, authdomain.ErrInvalidIDToken
	}
	return &authdomain.ExternalIdentity{
		Provider: provider, ProviderID: "provider-user-1", Email: "user@example.com",
		EmailVerified: true, DisplayName: "Test User",
	}, nil
}
```

Add `TestSocialLoginReturnsUsableEagleToken`, `TestRefreshRotatesToken`, and `TestLogoutRevokesRefreshToken`. The first test posts a valid provider token and then uses the returned Eagle access token on an authenticated/protected API. The refresh test asserts the old refresh token is rejected after rotation; the logout test asserts a revoked refresh token cannot be used. Provider signature, audience, and nonce cryptography remain covered with a real RS256 token in `internal/auth/infrastructure/token_test.go`.

- [ ] **Step 6: Update server construction and run focused tests**

Construct the verifier as:

```go
authn.NewVerifier(authn.Config{
	Issuer:        c.GetIssuer(),
	Audience:      c.GetAudience(),
	SigningSecret: c.GetSigningSecret(),
})
```

Run:

```bash
GOCACHE=/tmp/eagle-go-build-cache go test ./pkg/authn ./internal/auth/infrastructure ./pkg/platform/server ./tests/e2e
```

Expected: PASS.

- [ ] **Step 7: Commit the Eagle JWT-only flow**

```bash
git add pkg/authn pkg/platform/server/server.go internal/auth/infrastructure/token.go internal/auth/infrastructure/token_test.go tests/e2e
git commit -m "refactor(auth): keep only Eagle access tokens"
```

### Task 2: Simplify roles and make every authorization decision policy-driven

**Files:**
- Modify: `pkg/identity/identity.go`
- Modify: `pkg/authz/authz.go`
- Modify: `pkg/authz/authz_test.go`
- Modify: `internal/access/domain/rolebinding.go`
- Modify: `internal/access/domain/permission_test.go`
- Modify: `internal/access/infrastructure/policy.go`
- Modify: `internal/access/infrastructure/infrastructure_test.go`
- Modify: `tests/e2e/main_test.go`
- Modify: `tests/e2e/auth_test.go`
- Modify: `api/eagle/annotations/v1/perm.proto`
- Modify: `pkg/authz/policy.go`
- Modify: `pkg/authz/policy_test.go`
- Modify: `.agents/rules/api-authz.md`
- Regenerate: `api/eagle/annotations/v1/perm.pb.go`
- Regenerate: generated HTTP/OpenAPI files affected by descriptors

**Interfaces:**
- Produces: `identity.Principal{Subject string, Username string, Email string, Roles []string}`.
- Produces: `access/domain.NewRole(string) (Role, error)` accepting plain ASCII role keys.
- Produces: `authz.Server(authorizer Authorizer) middleware.Middleware` or equivalent single-authorizer construction without options.
- Preserves: `Authorizer.AllowContext(context.Context, []string, string) (bool, error)`.

- [ ] **Step 1: Write role-domain tests for plain role keys**

Add a table test with exact behavior:

```go
tests := []struct {
	value string
	want  bool
}{
	{"user", true},
	{"admin", true},
	{"support-agent", true},
	{"ops_2", true},
	{"", false},
	{"realm:admin", false},
	{"2admin", false},
	{"admin role", false},
}
```

Role keys must start with an ASCII letter, contain only ASCII letters, digits, `_` or `-`, and be at most 64 bytes.

- [ ] **Step 2: Write an authorization test proving admin has no bypass**

Remove `WithSuperAdminRole` expectations. Add a test whose principal has role `admin`, whose fake authorizer denies the permission, and assert 403 plus one authorizer call. Add a second case where the authorizer allows `admin` through `system:*` and assert success.

- [ ] **Step 3: Run domain and authz tests to verify failure**

Run: `GOCACHE=/tmp/eagle-go-build-cache go test ./internal/access/domain ./pkg/authz`

Expected: FAIL because role namespaces and the super-admin bypass still exist.

- [ ] **Step 4: Implement plain roles and remove legacy principal fields**

Delete `RealmRoleKey`, `ClientRoleKey`, `ValidRoleKey`, `HasClientRole`, prefix constants, `ClientID`, `Scopes`, and `ClientRoles`. Make `NewRole` enforce the test table. Map fixed token claims directly:

```go
return &identity.Principal{
	Subject:  c.Subject,
	Username: c.Username,
	Email:    c.Email,
	Roles:    slices.Clone(c.Roles),
}
```

The clone prevents downstream code from mutating verifier-owned claims.

- [ ] **Step 5: Remove the super-admin option and client audit metadata**

Delete `options.superAdminRole`, `WithSuperAdminRole`, and the bypass branch. Require the injected `Authorizer` for permission-required methods. In `internal/access/infrastructure/policy.go`, audit only `p.Subject`; remove reads of `p.ClientID`.

- [ ] **Step 6: Delete the legacy `public` annotation path**

In `perm.proto`, remove `bool public = 50002` and reserve field number 50002. Keep `access = 50003` and the reserved `ACCESS_LEVEL_INTERNAL` enum number. In `pkg/authz/policy.go`, derive public access only from `AccessLevel`; remove legacy descriptor fallback. Update tests so a missing `access` stays unknown/fail-closed.

- [ ] **Step 7: Regenerate API descriptors and update E2E role cases**

Run: `make api`

Update E2E helpers and policies to use `viewer`, `editor`, `admin`, and `user`. Delete tests for realm/client collision, foreign-client roles, and super-admin bypass. Add an E2E case proving `admin` succeeds only because the seeded Casbin policy contains `system:*`.

- [ ] **Step 8: Run focused authorization tests**

Run:

```bash
GOCACHE=/tmp/eagle-go-build-cache go test ./internal/access/... ./pkg/authz ./pkg/identity ./tests/e2e
```

Expected: PASS.

- [ ] **Step 9: Commit role and authorization simplification**

```bash
git add api/eagle/annotations/v1 pkg/identity pkg/authz internal/access tests/e2e openapi.yaml .agents/rules/api-authz.md
git commit -m "refactor(access): use policy-driven plain roles"
```

### Task 3: Remove obsolete auth configuration and composition wrappers

**Files:**
- Modify: `pkg/platform/config/config.proto`
- Modify: `pkg/platform/config/validate.go`
- Modify: `pkg/platform/config/config_test.go`
- Modify: `pkg/platform/runtime/runtime.go`
- Modify: `pkg/platform/server/server.go`
- Modify: `cmd/eagle/main.go`
- Modify: `cmd/eagle/app.go`
- Modify: `cmd/eagle/providers.go`
- Modify: `configs/config.yaml`
- Regenerate: `pkg/platform/config/config.pb.go`

**Interfaces:**
- Produces: `config.Validate(*config.Bootstrap) error`.
- Produces: `server.NewMiddlewares(logger, verifier, authorizer, errorMappings...)` without `*config.Auth`.
- Produces: `runtime.Spec{Name string, Version string, Build Builder}`.
- Consumes: the Task 1 token issuer and verifier signatures.

- [ ] **Step 1: Rewrite config tests around the minimal Auth message**

Delete JWKS, role-claim, client-id, and super-admin assertions. Keep exact failure cases for:

```text
auth.issuer is required or invalid
auth.audience is required
auth.signing_secret must be at least 32 bytes
auth access_token_ttl and refresh_token_ttl must be positive
auth.refresh_token_ttl must exceed access_token_ttl
auth.google.client_id is required when Google login is enabled
auth.apple.client_id is required when Apple login is enabled
```

Call only `config.Validate(bc)`.

- [ ] **Step 2: Run config tests to verify the obsolete API is exposed**

Run: `GOCACHE=/tmp/eagle-go-build-cache go test ./pkg/platform/config`

Expected: FAIL because the Proto and validator still expose removed fields and `Requirements`.

- [ ] **Step 3: Simplify the Auth config source and validator**

Keep existing field numbers for retained fields and reserve removed field numbers so generated descriptors cannot accidentally reuse old wire meanings:

```proto
message Auth {
  string issuer = 1;
  reserved 2, 4, 5, 6, 7, 8, 9, 10;
  string audience = 3;
  string signing_secret = 11;
  google.protobuf.Duration access_token_ttl = 12;
  google.protobuf.Duration refresh_token_ttl = 13;
  SocialProvider google = 14;
  SocialProvider apple = 15;
}
```

Delete `Requirements`, variadic validation, `validateJWKS`, client-role checks, and conditional signing-secret mode. Validate the whole Bootstrap every time.

- [ ] **Step 4: Regenerate config and update YAML**

Run: `make config`

Remove `AUTH_CLIENT_ID`, `AUTH_JWKS_*`, `AUTH_*_ROLES_CLAIM`, and `super_admin_role` from `configs/config.yaml`. Keep issuer, audience, signing secret, TTLs, and provider client IDs.

- [ ] **Step 5: Simplify runtime and composition**

Remove `Spec.Requirements`; call `config.Validate(&bc)`. Pass `composeApp` directly as `Spec.Build`, delete `buildApp`, remove the now-unused config import from `cmd/eagle/main.go`, and remove auth config from `NewMiddlewares`. Construct the token issuer without client ID.

- [ ] **Step 6: Run config, runtime, server, and command tests**

Run:

```bash
GOCACHE=/tmp/eagle-go-build-cache go test ./pkg/platform/config ./pkg/platform/runtime ./pkg/platform/server ./cmd/eagle ./tests/e2e
```

Expected: PASS.

- [ ] **Step 7: Commit configuration cleanup**

```bash
git add pkg/platform/config pkg/platform/runtime pkg/platform/server cmd/eagle configs/config.yaml
git commit -m "refactor(config): remove legacy OIDC settings"
```

### Task 4: Rebuild the pre-production schema as one minimal baseline

**Files:**
- Modify: `migrations/00001_baseline.sql`
- Delete: `migrations/00002_social_auth.sql`
- Modify: `internal/platform/database/ent/schema/casbinrule.go`
- Modify: `internal/platform/database/ent/schema/policyaudit.go`
- Modify: `internal/access/infrastructure/policy_store.go`
- Modify: `internal/access/infrastructure/policy.go`
- Modify: `internal/access/infrastructure/infrastructure_test.go`
- Modify: `internal/access/infrastructure/policy_sync_test.go`
- Modify: `tests/database/schema_test.go`
- Modify: `tests/architecture/data_ownership_test.go` only if generated selector names or ownership checks require it
- Regenerate: `internal/platform/database/ent/**`

**Interfaces:**
- Preserves: `authz.StoredPolicy{PType string, Values []string}` with exactly two values for `p` and `g` rows.
- Produces: `policyMutationMeta{actorSubject, requestID, traceID string}`.
- Produces: `newPolicyRule(client, ptype, v0, v1 string) *ent.CasbinRuleCreate`.

- [ ] **Step 1: Add schema assertions for removed columns and one migration version**

Extend database tests with an exact column-set helper:

```go
func assertExactColumnNames(t *testing.T, db *sql.DB, table string, want []string) {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1
		ORDER BY ordinal_position`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("%s columns = %v, want %v", table, got, want)
	}
}

assertExactColumnNames(t, db, "casbin_rule", []string{"id", "ptype", "v0", "v1"})
assertExactColumnNames(t, db, "authz_policy_audit", []string{
	"id", "policy_version", "action", "target", "actor_subject",
	"request_id", "trace_id", "before", "after", "created_at",
})
```

Assert goose reaches version 1 and both `social_identity` and `auth_session` exist after the baseline migration.

- [ ] **Step 2: Run schema tests to verify failure**

Run: `GOCACHE=/tmp/eagle-go-build-cache go test ./tests/database ./pkg/db`

Expected: FAIL because the legacy columns and second migration still exist.

- [ ] **Step 3: Update Ent schema sources**

Reduce `CasbinRule.Fields` to `id`, `ptype`, `v0`, and `v1`; make the unique index `(ptype, v0, v1)`. Remove `actor_client_id` from `PolicyAudit.Fields`. Do not edit generated Ent files.

- [ ] **Step 4: Rebuild the goose baseline**

Move `social_identity` and `auth_session` creation into `00001_baseline.sql`; update its Down section in foreign-key-safe reverse order. Reduce `casbin_rule` and its indexes to the actual columns, remove `actor_client_id`, and seed:

```sql
INSERT INTO casbin_rule (ptype, v0, v1) VALUES
    ('p', 'admin', 'system:*'),
    ('p', 'user',  'system:role:list'),
    ('p', 'user',  'system:role:query'),
    ('p', 'user',  'system:permission:list'),
    ('p', 'user',  'system:permission:query'),
    ('p', 'user',  'system:dict:list'),
    ('g', 'admin', 'user');
```

Delete `00002_social_auth.sql`.

- [ ] **Step 5: Simplify policy persistence code**

Load every row as `[]string{row.V0, row.V1}`. Replace six-slot construction with:

```go
func newPolicyRule(client *ent.Client, ptype, v0, v1 string) *ent.CasbinRuleCreate {
	return client.CasbinRule.Create().SetPtype(ptype).SetV0(v0).SetV1(v1)
}
```

Remove `trimTrailingEmpty`, `actorClientID`, and `.SetActorClientID`. Update all call sites and audit assertions.

- [ ] **Step 6: Regenerate Ent and run data tests**

Run:

```bash
make ent
GOCACHE=/tmp/eagle-go-build-cache go test ./internal/access/infrastructure ./internal/auth/infrastructure ./tests/database ./pkg/db ./tests/architecture
```

Expected: PASS, including migration `up -> down -> up` and Ent/goose consistency.

- [ ] **Step 7: Commit the minimal baseline**

```bash
git add migrations internal/platform/database/ent internal/access/infrastructure tests/database tests/architecture
git commit -m "refactor(database): rebuild minimal scaffold baseline"
```

### Task 5: Remove redundant dependency and synchronize deployment and documentation

**Files:**
- Modify: `pkg/platform/runtime/runtime.go`
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `README.md`
- Modify: `docs/architecture.md`
- Modify: `docs/social-login.md`
- Modify: `docs/development-deployment.md`
- Modify: `docs/aliyun-flow-deployment.md`
- Modify: `deploy/scripts/deploy.sh`
- Modify: `deploy/environments/development.env.example`
- Modify: `deploy/environments/testing.env.example`
- Delete: `docs/superpowers/specs/2026-09-02-monolith-scaffold-pruning-design.md`
- Delete: `docs/superpowers/plans/2026-09-02-monolith-scaffold-pruning.md`

**Interfaces:**
- Preserves: deployment environment variables for database, Eagle JWT, Google/Apple, HTTP, logs, metrics, and OTLP.
- Removes: all deployment variables for external access-token JWKS and dynamic role claims.

- [ ] **Step 1: Remove the runtime side-effect import**

Delete:

```go
_ "go.uber.org/automaxprocs"
```

Do not replace it; Go 1.27 provides container-aware `GOMAXPROCS` defaults.

- [ ] **Step 2: Remove obsolete deployment variables**

Delete `EAGLE_AUTH_CLIENT_ID`, `EAGLE_AUTH_JWKS_URL`, `EAGLE_AUTH_JWKS_PATH`, `EAGLE_AUTH_REALM_ROLES_CLAIM`, `EAGLE_AUTH_CLIENT_ROLES_CLAIM`, and super-admin configuration from scripts, env examples, tables, and prose. Keep `EAGLE_AUTH_ISSUER`, `EAGLE_AUTH_AUDIENCE`, `EAGLE_AUTH_SIGNING_SECRET`, TTLs, and Google/Apple client IDs.

- [ ] **Step 3: Rewrite architecture and login descriptions around the single token flow**

Every overview must describe:

```text
Google/Apple ID Token -> auth verification -> local session -> Eagle JWT -> authn -> authz -> Casbin
```

Replace realm/client role examples with `user`, `admin`, `viewer`, or `editor`. State that phone login is an adapter extension point, not an installed capability. Remove Wire and external resource-server wording.

- [ ] **Step 4: Remove superseded design artifacts**

Delete the 2026-09-02 pruning spec and plan because they describe Wire, external OIDC, and a repository state that no longer exists. Keep the current 2026-09-07 spec and plan.

- [ ] **Step 5: Tidy both Go modules and inspect the dependency diff**

Run:

```bash
GOCACHE=/tmp/eagle-go-build-cache go mod tidy
GOCACHE=/tmp/eagle-go-build-cache go -C tools mod tidy
git diff -- go.mod go.sum tools/go.mod tools/go.sum
```

Expected: `go.uber.org/automaxprocs` and its now-unreachable module entries disappear; no mature dependency listed in the spec disappears unless `go mod tidy` proves it has no caller.

- [ ] **Step 6: Search for forbidden leftovers**

Run:

```bash
rg -n 'AUTH_JWKS|jwks_path|jwks_url|realm_roles_claim|client_roles_claim|RealmRoleKey|ClientRoleKey|HasClientRole|super_admin_role|WithSuperAdminRole|automaxprocs|external OIDC|外部 OIDC|Wire' --glob '!docs/superpowers/specs/2026-09-07-modular-monolith-scaffold-pruning-design.md' --glob '!docs/superpowers/plans/2026-09-07-modular-monolith-scaffold-pruning.md' .
```

Expected: no matches outside provider-side Google/Apple JWKS implementation and intentional historical field-number comments. Any allowed provider match must be in `internal/auth/infrastructure` or its tests.

- [ ] **Step 7: Commit dependency and documentation cleanup**

```bash
git add pkg/platform/runtime/runtime.go go.mod go.sum README.md docs deploy
git commit -m "docs: align scaffold with local auth architecture"
```

### Task 6: Regenerate, audit, and verify the complete scaffold

**Files:**
- Modify only if verification exposes an in-scope inconsistency: generated API/config/Ent outputs, source tests, docs, `go.mod`, or `go.sum`
- Verify: all repository files

**Interfaces:**
- Consumes all interfaces produced by Tasks 1–5.
- Produces a clean generated tree and evidence that source, schema, dependencies, deployment, and documentation agree.

- [ ] **Step 1: Format every modified Go source**

Run `gofmt -w` with the explicit modified non-generated Go file list from `git diff --name-only -- '*.go'`, excluding `*.pb.go` and `internal/platform/database/ent/**`.

- [ ] **Step 2: Run the complete generation pipeline**

Run: `make generate`

Expected: API, config, Ent, OpenAPI, root dependencies, and tools dependencies regenerate without error.

- [ ] **Step 3: Verify generation is idempotent**

Record `git status --short`, run `make generate` a second time, and confirm the second run adds no new diff.

- [ ] **Step 4: Run fast package and architecture tests**

Run:

```bash
GOCACHE=/tmp/eagle-go-build-cache go test -short ./...
GOCACHE=/tmp/eagle-go-build-cache go test ./tests/architecture/...
```

Expected: PASS.

- [ ] **Step 5: Run lint, race/integration tests, and deployment validation**

Run:

```bash
make lint
make test
make validate-deploy
```

Expected: PASS. If a command is blocked by unavailable Docker, network, or sandbox access, record the exact failure and run every unaffected check.

- [ ] **Step 6: Audit direct dependencies and removed concepts**

For each direct requirement in root `go.mod`, locate at least one import with `rg` or generated code. Repeat the forbidden-leftover search from Task 5 and run `git diff --check`.

Expected: every direct dependency has a current production, test, generated, or tool caller; no whitespace errors or obsolete architecture concepts remain.

- [ ] **Step 7: Commit verification fixes, if any**

```bash
git add -A
git commit -m "test: verify pruned modular monolith scaffold"
```

Skip this commit only if verification produced no tracked changes after Task 5.
