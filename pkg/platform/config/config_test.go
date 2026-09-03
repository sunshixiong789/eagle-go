package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	kratosconfig "github.com/go-kratos/kratos/v3/config"
	configenv "github.com/go-kratos/kratos/v3/config/env"
	"github.com/go-kratos/kratos/v3/config/file"

	appconfig "github.com/eagle-go/eagle/pkg/platform/config"
)

// configDir 是仓库根下唯一的配置目录。
func configDir(t *testing.T) string {
	t.Helper()

	path, err := filepath.Abs(filepath.Join("..", "..", "..", "configs"))
	if err != nil {
		t.Fatalf("解析配置目录: %v", err)
	}
	return path
}

// 配置文件里的字段名拼错不会导致编译失败——proto 解析时对不上的键
// 会被静默丢弃，直到运行时发现某个功能"没配置"才暴露。
// 这些测试真实加载 configs/config.yaml，把这类错误提前到 CI。
func loadConfig(t *testing.T) *appconfig.Bootstrap {
	t.Helper()

	c := kratosconfig.New(
		kratosconfig.WithSource(file.NewSource(configDir(t))),
		kratosconfig.WithResolveActualTypes(true),
	)
	t.Cleanup(func() { _ = c.Close() })

	if err := c.Load(); err != nil {
		t.Fatalf("加载配置: %v", err)
	}

	var bc appconfig.Bootstrap
	if err := c.Scan(&bc); err != nil {
		t.Fatalf("解析配置到 proto: %v", err)
	}
	return &bc
}

func TestEnvironmentOverridesSensitiveDefaults(t *testing.T) {
	path := configDir(t)
	t.Setenv("EAGLE_DATABASE_DSN", "postgres://runtime:secret@db.internal:5432/eagle?sslmode=require")
	t.Setenv("EAGLE_SERVER_HTTP_ADDR", "0.0.0.0:18000")
	t.Setenv("EAGLE_AUTH_JWKS_PATH", "/.well-known/jwks.json")
	t.Setenv("EAGLE_AUTH_REALM_ROLES_CLAIM", "https://eagle.example.com/roles")
	t.Setenv("EAGLE_OBSERVABILITY_TRACE_SAMPLE_RATIO", "0.05")

	c := kratosconfig.New(
		kratosconfig.WithSource(file.NewSource(path), configenv.NewSource("EAGLE")),
		kratosconfig.WithResolveActualTypes(true),
	)
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Load(); err != nil {
		t.Fatalf("加载配置: %v", err)
	}
	var bc appconfig.Bootstrap
	if err := c.Scan(&bc); err != nil {
		t.Fatalf("解析配置: %v", err)
	}
	if got := bc.GetData().GetDatabase().GetDsn(); !strings.Contains(got, "runtime:secret@db.internal") {
		t.Fatalf("DATABASE_DSN override not applied: %q", got)
	}
	if got := bc.GetServer().GetHttp().GetAddr(); got != "0.0.0.0:18000" {
		t.Fatalf("SERVER_HTTP_ADDR override not applied: %q", got)
	}
	if got := bc.GetAuth().GetJwksPath(); got != "/.well-known/jwks.json" {
		t.Fatalf("AUTH_JWKS_PATH override not applied: %q", got)
	}
	if got := bc.GetAuth().GetRealmRolesClaim(); got != "https://eagle.example.com/roles" {
		t.Fatalf("AUTH_REALM_ROLES_CLAIM override not applied: %q", got)
	}
	if got := bc.GetObservability().GetTraceSampleRatio(); got != 0.05 {
		t.Fatalf("OBSERVABILITY_TRACE_SAMPLE_RATIO override not applied: %v", got)
	}
}

func TestConfigParses(t *testing.T) {
	bc := loadConfig(t)
	if err := appconfig.Validate(bc, appconfig.Requirements{Database: true, Auth: true, HTTP: true}); err != nil {
		t.Fatalf("配置校验失败: %v", err)
	}
	if got := bc.GetServer().GetHttp().GetAddr(); got != "0.0.0.0:8000" {
		t.Errorf("server.http.addr = %q, want %q", got, "0.0.0.0:8000")
	}
	if got := bc.GetObservability().GetMetricsAddr(); got != "0.0.0.0:9101" {
		t.Errorf("observability.metrics_addr = %q, want %q", got, "0.0.0.0:9101")
	}
	if got := bc.GetData().GetDatabase().GetDsn(); !strings.Contains(got, "/eagle?") {
		t.Errorf("data.database.dsn = %q, want database %q", got, "eagle")
	}
}

func TestAuthConfigRequiresProviderSpecificLocations(t *testing.T) {
	bc := &appconfig.Bootstrap{Auth: &appconfig.Auth{
		Issuer:         "https://idp.example.com",
		ClientId:       "eagle-api",
		Audience:       "eagle-api",
		SuperAdminRole: "admin",
	}}

	err := appconfig.Validate(bc, appconfig.Requirements{Auth: true})
	if err == nil {
		t.Fatal("未配置 JWKS 和角色 claim 时应拒绝启动")
	}
	message := err.Error()
	for _, want := range []string{"auth.jwks_url or auth.jwks_path is required", "auth.client_roles_claim is required"} {
		if !strings.Contains(message, want) {
			t.Errorf("Validate error = %q, want %q", message, want)
		}
	}
}

// issuer 必须与 IdP 签发的 iss 完全一致，否则所有 token 都会因 iss
// 不匹配被拒——而报错信息不会指向这里，很难定位。
//
// 这里刻意不断言 issuer 的路径形态，避免把 IdP 供应商焊死。
func TestAuthConfigIsUsable(t *testing.T) {
	auth := loadConfig(t).GetAuth()

	issuer := auth.GetIssuer()
	if issuer == "" {
		t.Fatal("auth.issuer 未配置，token 验签无法进行")
	}
	// 尾斜杠会让 iss 比对失败，且肉眼极难发现
	if strings.HasSuffix(issuer, "/") {
		t.Errorf("issuer = %q 带了尾斜杠，与 token 里的 iss 对不上", issuer)
	}

	if auth.GetClientId() == "" {
		t.Error("auth.client_id 未配置，取不到本服务的 client 角色")
	}
	if auth.GetSuperAdminRole() == "" {
		t.Error("auth.super_admin_role 未配置")
	}
	if auth.GetJwksUrl() == "" && auth.GetJwksPath() == "" {
		t.Error("auth.jwks_url 与 auth.jwks_path 至少配置一个")
	}
	// 相对 JWKS 路径必须以 / 开头，否则会与 issuer 拼出错误地址。
	if jwksPath := auth.GetJwksPath(); jwksPath != "" && !strings.HasPrefix(jwksPath, "/") {
		t.Errorf("auth.jwks_path = %q，必须以 / 开头", jwksPath)
	}
	if auth.GetClientRolesClaim() == "" {
		t.Error("auth.client_roles_claim 未配置")
	}
}

// 采样率越界会被 otelx 收敛到 [0,1]，但配置文件本身不该写错。
func TestObservabilityConfigIsSane(t *testing.T) {
	bc := loadConfig(t)
	o := bc.GetObservability()

	if r := o.GetTraceSampleRatio(); r < 0 || r > 1 {
		t.Errorf("trace_sample_ratio = %v, 应在 [0,1] 区间", r)
	}
	if o.GetMetricsAddr() == "" {
		t.Error("metrics_addr 未配置，Prometheus 抓不到指标")
	}
	if o.GetOtlpEndpoint() != "" && !o.GetOtlpInsecure() {
		t.Error("本地无 TLS collector 配置应显式启用 otlp_insecure")
	}
	// 指标端点必须与业务端口分开：它不经过认证鉴权中间件，
	// 与业务共用端口就等于给业务服务开了个免鉴权的口子
	if o.GetMetricsAddr() == bc.GetServer().GetHttp().GetAddr() {
		t.Error("metrics_addr 与业务 HTTP 端口相同，指标端点会绕过鉴权暴露业务接口")
	}
}
