package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/eagle-go/eagle/internal/platform/database/ent/accountroleaudit"
)

func TestAccountRolesAuthorizationAndRevision(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	const subject = "e2e-account-roles"
	if _, err := env.db.Client().UserAccount.Create().SetID(subject).Save(ctx); err != nil {
		t.Fatal(err)
	}
	admin := env.userToken(t, "account-operator", "admin")
	path := "/v1/system/accounts/" + subject + "/roles"
	for _, tc := range []struct {
		token string
		want  int
	}{{"", 401}, {env.userToken(t, "account-reader", "user"), 403}, {admin, 200}} {
		for _, route := range []string{"/v1/system/accounts", path} {
			if code, body := env.get(t, route, tc.token); code != tc.want {
				t.Fatalf("GET %s = %d %s", route, code, body)
			}
		}
	}
	_, body := env.get(t, path, admin)
	var state struct {
		Revision int64 `json:"revision"`
	}
	if err := json.Unmarshal([]byte(body), &state); err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf(`{"roles":["user"],"expected_revision":%d}`, state.Revision)
	for _, tc := range []struct {
		token string
		want  int
	}{{"", 401}, {env.userToken(t, "account-reader", "user"), 403}, {admin, 200}} {
		if code, body := env.do(t, http.MethodPut, path, tc.token, payload); code != tc.want {
			t.Fatalf("PUT = %d %s, want %d", code, body, tc.want)
		}
	}
	if code, body := env.do(t, http.MethodPut, path, admin, payload); code != 409 {
		t.Fatalf("stale PUT = %d %s", code, body)
	}
	if code, body := env.do(t, http.MethodPut, path, admin, `{"roles":["*"],"expected_revision":1}`); code != 400 {
		t.Fatalf("invalid PUT = %d %s", code, body)
	}
	if code, body := env.get(t, "/v1/system/accounts/not-an-account/roles", admin); code != 404 {
		t.Fatalf("missing GET = %d %s", code, body)
	}
	audit, err := env.db.Client().AccountRoleAudit.Query().Where(accountroleaudit.AccountSubjectEQ(subject)).Only(ctx)
	if err != nil || audit.ActorSubject != "subject-account-operator" || audit.Action != "roles.replace" {
		t.Fatalf("audit = %+v %v", audit, err)
	}
}
