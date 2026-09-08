package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

// get 带可选 token 发起请求，返回状态码与响应体。
func (e *testEnv) get(t *testing.T, path, token string) (int, string) {
	t.Helper()
	return e.do(t, http.MethodGet, path, token, "")
}

func (e *testEnv) do(t *testing.T, method, path, token, body string) (int, string) {
	t.Helper()

	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, e.http.URL+path, r)
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := e.http.Client().Do(req)
	if err != nil {
		t.Fatalf("发起请求: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// 服务能不能起来、路由通不通——这是此前从未验证过的部分。
// 上一轮的 Duration 配置缺陷正是因为从没跑过完整启动路径才漏掉的。
func TestServiceBootsAndRoutes(t *testing.T) {
	env := newTestEnv(t)

	// 未带 token 访问受保护接口，应当是 401 而不是 404 或 500。
	// 404 说明路由没注册，500 说明中间件链装配有问题。
	code, body := env.get(t, "/v1/system/permissions", "")
	if code != http.StatusUnauthorized {
		t.Errorf("未认证请求 = %d (%s), want 401", code, body)
	}
}

func TestPublicJWKSExposesOnlyVerificationMaterial(t *testing.T) {
	env := newTestEnv(t)
	code, body := env.get(t, "/.well-known/jwks.json", "")
	if code != http.StatusOK {
		t.Fatalf("JWKS = %d (%s), want 200", code, body)
	}
	var document struct {
		Keys []map[string]any `json:"keys"`
	}
	if err := json.Unmarshal([]byte(body), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Keys) != 1 || document.Keys[0]["kid"] != testKeyID ||
		document.Keys[0]["alg"] != "ES256" || document.Keys[0]["d"] != nil {
		t.Fatalf("unexpected JWKS: %s", body)
	}
}

func TestUnauthenticatedIsRejected(t *testing.T) {
	env := newTestEnv(t)

	cases := []struct {
		name  string
		token string
	}{
		{"无 token", ""},
		{"伪造签名", mintEagleToken(t, tokenOpts{
			subject: "s1",
			roles:   []string{"admin"}, wrongKey: true,
		})},
		{"已过期", mintEagleToken(t, tokenOpts{
			subject: "s2",
			roles:   []string{"admin"}, expiresIn: -time.Hour,
		})},
		{"aud 不匹配", mintEagleToken(t, tokenOpts{
			subject: "subject-3",
			roles:   []string{"admin"}, audience: []string{"another-service"},
		})},
		{"尚未生效", mintEagleToken(t, tokenOpts{
			subject: "s4",
			roles:   []string{"admin"}, notBefore: 10 * time.Minute,
		})},
		{"非白名单签名算法", mintEagleToken(t, tokenOpts{
			subject: "s5",
			roles:   []string{"admin"}, algorithm: jose.HS512,
		})},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, body := env.get(t, "/v1/system/permissions", c.token)
			if code != http.StatusUnauthorized {
				t.Errorf("= %d (%s), want 401", code, body)
			}
		})
	}
}

// 这是整套鉴权的核心语义：角色对了才放行，错了必须 403 而不是 401。
// 401 与 403 混淆会让前端无法区分「该重新登录」和「你没这个权限」。
func TestAuthorizationByRole(t *testing.T) {
	env := newTestEnv(t)

	// viewer 只给读权限，不给写
	env.grantRole(t, "viewer", "system:permission:list", "system:permission:query")
	env.grantRole(t, "editor", "system:permission:list", "system:permission:add")

	t.Run("有权限则放行", func(t *testing.T) {
		token := userToken(t, "vera", "viewer")
		code, body := env.get(t, "/v1/system/permissions", token)
		if code != http.StatusOK {
			t.Errorf("viewer 读权限列表 = %d (%s), want 200", code, body)
		}
	})

	t.Run("缺权限则 403", func(t *testing.T) {
		token := userToken(t, "vera", "viewer")
		code, body := env.do(t, http.MethodPost, "/v1/system/permissions", token,
			`{"name":"测试","type":1}`)
		if code != http.StatusForbidden {
			t.Errorf("viewer 尝试新增 = %d (%s), want 403", code, body)
		}
	})

	t.Run("换个有权限的角色就通过", func(t *testing.T) {
		token := userToken(t, "eddie", "editor")
		code, body := env.do(t, http.MethodPost, "/v1/system/permissions", token,
			`{"name":"E2E 测试节点","type":1}`)
		if code != http.StatusOK {
			t.Errorf("editor 新增 = %d (%s), want 200", code, body)
		}
	})

	t.Run("无任何角色一律 403", func(t *testing.T) {
		token := userToken(t, "nobody")
		code, body := env.get(t, "/v1/system/permissions", token)
		if code != http.StatusForbidden {
			t.Errorf("无角色用户 = %d (%s), want 403", code, body)
		}
	})
}

// 权限变更后重载策略应立即生效，不必重启服务。
func TestPolicyReloadTakesEffect(t *testing.T) {
	env := newTestEnv(t)
	env.grantRole(t, "dynamic", "system:permission:list")

	token := userToken(t, "dyn", "dynamic")
	if code, _ := env.get(t, "/v1/system/permissions", token); code != http.StatusOK {
		t.Fatal("初始应有权限")
	}

	// 收回权限
	env.grantRole(t, "dynamic")

	if code, body := env.get(t, "/v1/system/permissions", token); code != http.StatusForbidden {
		t.Errorf("收权后 = %d (%s), want 403", code, body)
	}
}

// 错误响应必须是结构化 JSON 且带 reason，前端据此分支处理。
func TestErrorResponseShape(t *testing.T) {
	env := newTestEnv(t)
	env.grantRole(t, "viewer", "system:permission:list")

	token := userToken(t, "vera", "viewer")
	code, body := env.do(t, http.MethodPost, "/v1/system/permissions", token,
		`{"name":"x","type":3,"code":"a:b:c"}`)
	if code != http.StatusForbidden {
		t.Fatalf("= %d, want 403", code)
	}

	var e struct {
		Code    int    `json:"code"`
		Reason  string `json:"reason"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		t.Fatalf("错误响应不是合法 JSON: %v (body=%s)", err, body)
	}
	if e.Reason != "FORBIDDEN" {
		t.Errorf("reason = %q, want FORBIDDEN (body=%s)", e.Reason, body)
	}
	if e.Message == "" {
		t.Error("message 为空，前端无从展示")
	}
}
