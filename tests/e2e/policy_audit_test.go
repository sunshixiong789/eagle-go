package e2e

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/eagle-go/eagle/internal/platform/database/ent/policyaudit"
)

// 策略的三种写入都必须把验签后的主体和入站请求标识传递到事务审计。
func TestPolicyWritesAuditAuthenticatedActor(t *testing.T) {
	env := newTestEnv(t)
	token := env.userToken(t, "policy-operator", "admin")
	for _, tc := range []struct{ name, method, path, body string }{
		{"permissions", http.MethodPut, "/v1/system/role-bindings/audit-editor", `{"permission_codes":["system:dict:list"]}`},
		{"inherit", http.MethodPost, "/v1/system/role-bindings/inheritance", `{"child":"audit-editor","parent":"audit-viewer"}`},
		{"remove-inheritance", http.MethodDelete, "/v1/system/role-inheritances/audit-editor/audit-viewer", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), tc.method, env.http.URL+tc.path, strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			requestID := "audit-request-" + tc.name
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Request-ID", requestID)
			resp, err := env.http.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status=%d body=%s", resp.StatusCode, body)
			}
			audit, err := env.db.Client().PolicyAudit.Query().Where(policyaudit.RequestIDEQ(requestID)).Only(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if audit.ActorSubject != "subject-policy-operator" {
				t.Fatalf("actor = %q", audit.ActorSubject)
			}
		})
	}
}
