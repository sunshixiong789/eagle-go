package infrastructure

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eagle-go/eagle/internal/auth/domain"
	"github.com/eagle-go/eagle/internal/platform/database/ent/accountroleaudit"
)

func TestAccountRoleTransactions(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	ctx := context.Background()
	repo := NewAccountRepository(authTestDB, "account-transactions")
	for _, subject := range []string{"role-admin-a", "role-admin-b"} {
		if _, err := authTestDB.Client().UserAccount.Create().SetID(subject).SetDisplayName("Role Test").Save(ctx); err != nil {
			t.Fatal(err)
		}
	}
	results := make(chan error, 2)
	for _, subject := range []string{"role-admin-a", "role-admin-b"} {
		go func() { results <- repo.BootstrapAdmin(ctx, subject, "operator") }()
	}
	successes := 0
	for range 2 {
		err := <-results
		if err == nil {
			successes++
		} else if !errors.Is(err, domain.ErrAdminAlreadyInitialized) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("bootstrap successes = %d", successes)
	}
	a, err := repo.Roles(ctx, "role-admin-a")
	if err != nil {
		t.Fatal(err)
	}
	admin, other := "role-admin-a", "role-admin-b"
	if !slices.Contains(a.Roles, "admin") {
		admin, other = other, admin
	}
	state, err := repo.Roles(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Replace(ctx, domain.RoleAssignment{Subject: admin, ExpectedRevision: state.Revision, Actor: "operator"}); !errors.Is(err, domain.ErrLastAdmin) {
		t.Fatalf("last admin: %v", err)
	}
	revision, err := repo.Replace(ctx, domain.RoleAssignment{Subject: other, Roles: []string{"admin"}, ExpectedRevision: state.Revision, Actor: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Replace(ctx, domain.RoleAssignment{Subject: admin, ExpectedRevision: state.Revision, Actor: "operator"}); !errors.Is(err, domain.ErrRoleRevisionConflict) {
		t.Fatalf("stale revision: %v", err)
	}
	// 审计字段校验在角色删除之后失败，整个事务必须回滚。
	if _, err := repo.Replace(ctx, domain.RoleAssignment{Subject: admin, ExpectedRevision: revision, Actor: strings.Repeat("x", 129)}); err == nil {
		t.Fatal("expected audit write failure")
	}
	state, err = repo.Roles(ctx, admin)
	if err != nil || state.Revision != revision || !slices.Contains(state.Roles, "admin") {
		t.Fatalf("rollback: %+v %v", state, err)
	}
	// 同一版本并发降级两个管理员，只有一次提交；另一请求不能越过最后管理员保护。
	for _, subject := range []string{admin, other} {
		go func() {
			_, err := repo.Replace(ctx, domain.RoleAssignment{Subject: subject, ExpectedRevision: revision, Actor: "operator"})
			results <- err
		}()
	}
	successes = 0
	for range 2 {
		err := <-results
		if err == nil {
			successes++
		} else if !errors.Is(err, domain.ErrRoleRevisionConflict) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("replace successes = %d", successes)
	}
	audits, err := authTestDB.Client().AccountRoleAudit.Query().Where(accountroleaudit.AudienceEQ("account-transactions")).All(ctx)
	if err != nil || len(audits) != 3 {
		t.Fatalf("audits: %d %v", len(audits), err)
	}
	for _, audit := range audits {
		if audit.ActorSubject != "operator" {
			t.Fatalf("actor = %q", audit.ActorSubject)
		}
	}
	rows, total, err := repo.List(ctx, domain.AccountQuery{Keyword: "Role Test", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("list: %d / %d", len(rows), total)
	}
	if _, err := repo.Roles(ctx, "missing"); !errors.Is(err, domain.ErrAccountNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestEmptyAccountRolesSurviveLoginAndRefresh(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	ctx := context.Background()
	sessions := NewSessionRepository(authTestDB, &testIssuer{}, "empty-role-app")
	external := &domain.ExternalIdentity{Provider: domain.ProviderGoogle, ProviderID: "empty-role-provider"}
	grant, err := sessions.Create(ctx, external, "empty-role-account", domain.Session{ID: "empty-role-session", RefreshTokenHash: "empty-role-refresh", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewAccountRepository(authTestDB, "empty-role-app")
	state, err := repo.Roles(ctx, grant.Identity.Subject)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Replace(ctx, domain.RoleAssignment{Subject: grant.Identity.Subject, ExpectedRevision: state.Revision, Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	rotated, err := sessions.Rotate(ctx, "empty-role-refresh", "empty-role-next", time.Now().Add(time.Hour))
	if err != nil || len(rotated.Identity.Roles) != 0 {
		t.Fatalf("refresh restored roles: %+v %v", rotated, err)
	}
	login, err := sessions.Create(ctx, external, "unused-account", domain.Session{ID: "empty-role-login", RefreshTokenHash: "empty-role-login-hash", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil || len(login.Identity.Roles) != 0 {
		t.Fatalf("login restored roles: %+v %v", login, err)
	}
	other := NewSessionRepository(authTestDB, &testIssuer{}, "other-role-app")
	login, err = other.Create(ctx, external, "unused-account", domain.Session{ID: "other-role-login", RefreshTokenHash: "other-role-login-hash", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil || !slices.Equal(login.Identity.Roles, []string{"user"}) {
		t.Fatalf("audience isolation: %+v %v", login, err)
	}
}
