package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	kratosconfig "github.com/go-kratos/kratos/v3/config"
	configenv "github.com/go-kratos/kratos/v3/config/env"
	"github.com/go-kratos/kratos/v3/config/file"

	appconfig "github.com/eagle-go/eagle/internal/platform/config"
)

// 配置文件里的字段名拼错不会导致编译失败——proto 解析时对不上的键
// 会被静默丢弃，直到运行时发现某个功能"没配置"才暴露。
// 这个测试真实加载 configs/config.yaml，把这类错误提前到 CI。
func loadConfig(t *testing.T) *appconfig.Bootstrap {
	t.Helper()

	path, err := filepath.Abs(filepath.Join("..", "..", "..", "configs"))
	if err != nil {
		t.Fatalf("解析配置目录: %v", err)
	}

	c := kratosconfig.New(kratosconfig.WithSource(file.NewSource(path)))
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
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "configs"))
	if err != nil {
		t.Fatalf("解析配置目录: %v", err)
	}
	t.Setenv("EAGLE_DATABASE_DSN", "postgres://runtime:secret@db.internal:5432/eagle?sslmode=require")

	c := kratosconfig.New(kratosconfig.WithSource(file.NewSource(path), configenv.NewSource("EAGLE")))
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
}

func TestConfigParses(t *testing.T) {
	bc := loadConfig(t)
	if err := appconfig.Validate(bc); err != nil {
		t.Fatalf("配置校验失败: %v", err)
	}

	if bc.GetServer().GetHttp().GetAddr() == "" {
		t.Error("server.http.addr 未解析出来")
	}
	if bc.GetServer().GetGrpc().GetAddr() == "" {
		t.Error("server.grpc.addr 未解析出来")
	}
	if bc.GetData().GetDatabase().GetDsn() == "" {
		t.Error("data.database.dsn 未解析出来")
	}
	if bc.GetFile().GetLocalDir() == "" || bc.GetFile().GetMaxSizeBytes() <= 0 {
		t.Error("file 本地存储配置未解析出来")
	}
}

// issuer 必须与 Keycloak realm 的 iss 完全一致，否则所有 token
// 都会因 iss 不匹配被拒——而报错信息不会指向这里，很难定位。
func TestAuthConfigPointsAtKeycloakRealm(t *testing.T) {
	auth := loadConfig(t).GetAuth()

	issuer := auth.GetIssuer()
	if issuer == "" {
		t.Fatal("auth.issuer 未配置，token 验签无法进行")
	}
	if !strings.Contains(issuer, "/realms/") {
		t.Errorf("issuer = %q，Keycloak 的 issuer 形如 http(s)://host/realms/<realm>", issuer)
	}
	// 尾斜杠会让 iss 比对失败，且肉眼极难发现
	if strings.HasSuffix(issuer, "/") {
		t.Errorf("issuer = %q 带了尾斜杠，与 token 里的 iss 对不上", issuer)
	}

	if auth.GetClientId() == "" {
		t.Error("auth.client_id 未配置，取不到 resource_access 里的 client 角色")
	}
	if auth.GetSuperAdminRole() == "" {
		t.Error("auth.super_admin_role 未配置")
	}
}

// 采样率越界会被 otelx 收敛到 [0,1]，但配置文件本身不该写错。
func TestObservabilityConfigIsSane(t *testing.T) {
	o := loadConfig(t).GetObservability()

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
	bc := loadConfig(t)
	if o.GetMetricsAddr() == bc.GetServer().GetHttp().GetAddr() {
		t.Error("metrics_addr 与业务 HTTP 端口相同，指标端点会绕过鉴权暴露业务接口")
	}
}
