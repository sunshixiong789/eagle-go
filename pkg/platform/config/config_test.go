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

// configDir 返回包含所有环境共用模板的配置目录。
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
	t.Setenv("EAGLE_DATABASE_DRIVER", "mysql")
	t.Setenv("EAGLE_DATABASE_DSN", "runtime:secret@tcp(db.internal:3306)/eagle?parseTime=true&loc=UTC")
	t.Setenv("EAGLE_SERVER_HTTP_ADDR", "0.0.0.0:18000")
	t.Setenv("EAGLE_SERVER_SWAGGER_ENABLED", "true")
	t.Setenv("EAGLE_SERVER_SWAGGER_PATH", "/dev/docs")
	t.Setenv("EAGLE_AUTH_SIGNING_KEY_DIRECTORY", "/run/secrets/eagle-jwt-keys")
	t.Setenv("EAGLE_AUTH_ACTIVE_SIGNING_KEY_ID", "runtime-2026-09")
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
	if got := bc.GetData().GetDatabase().GetDriver(); got != "mysql" {
		t.Fatalf("DATABASE_DRIVER override not applied: %q", got)
	}
	if got := bc.GetData().GetDatabase().GetDsn(); !strings.Contains(got, "runtime:secret@tcp(db.internal:3306)") {
		t.Fatalf("DATABASE_DSN override not applied: %q", got)
	}
	if got := bc.GetServer().GetHttp().GetAddr(); got != "0.0.0.0:18000" {
		t.Fatalf("SERVER_HTTP_ADDR override not applied: %q", got)
	}
	if c := bc.GetServer().GetSwagger(); !c.GetEnabled() || c.GetPath() != "/dev/docs" || c.GetSpecFile() != "openapi.yaml" {
		t.Fatalf("Swagger environment overrides not applied: %v", c)
	}
	if got := bc.GetAuth().GetSigningKeyDirectory(); got != "/run/secrets/eagle-jwt-keys" {
		t.Fatalf("AUTH_SIGNING_KEY_DIRECTORY override not applied: %q", got)
	}
	if got := bc.GetAuth().GetActiveSigningKeyId(); got != "runtime-2026-09" {
		t.Fatalf("AUTH_ACTIVE_SIGNING_KEY_ID override not applied: %q", got)
	}
	if got := bc.GetObservability().GetTraceSampleRatio(); got != 0.05 {
		t.Fatalf("OBSERVABILITY_TRACE_SAMPLE_RATIO override not applied: %v", got)
	}
	if err := appconfig.Validate(&bc); err != nil {
		t.Fatalf("environment-overridden config rejected: %v", err)
	}
}

func TestConfigParses(t *testing.T) {
	bc := loadConfig(t)
	if c := bc.GetServer().GetSwagger(); c.GetEnabled() || c.GetPath() != "/swagger" || c.GetSpecFile() != "openapi.yaml" {
		t.Fatalf("Swagger must default to disabled with the standard path and spec: %v", c)
	}
	if err := appconfig.Validate(bc); err != nil {
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
	if got := bc.GetData().GetDatabase().GetDriver(); got != "postgres" {
		t.Errorf("data.database.driver = %q, want postgres", got)
	}
}

func TestDatabaseConfigAllowsMySQL(t *testing.T) {
	bc := loadConfig(t)
	database := bc.GetData().GetDatabase()
	database.Driver = "mysql"
	database.Dsn = "eagle:eagle@tcp(127.0.0.1:3306)/eagle?parseTime=true&loc=UTC"
	if err := appconfig.Validate(bc); err != nil {
		t.Fatalf("mysql configuration rejected: %v", err)
	}
}

func TestDatabaseConfigRejectsInvalidDriverAndMySQLTimeParsing(t *testing.T) {
	t.Run("driver", func(t *testing.T) {
		bc := loadConfig(t)
		bc.GetData().GetDatabase().Driver = "sqlite"
		if err := appconfig.Validate(bc); err == nil || !strings.Contains(err.Error(), "unsupported database driver") {
			t.Fatalf("Validate() error = %v", err)
		}
	})
	t.Run("parse time", func(t *testing.T) {
		bc := loadConfig(t)
		database := bc.GetData().GetDatabase()
		database.Driver = "mysql"
		database.Dsn = "eagle:eagle@tcp(127.0.0.1:3306)/eagle"
		if err := appconfig.Validate(bc); err == nil || !strings.Contains(err.Error(), "parseTime=true") {
			t.Fatalf("Validate() error = %v", err)
		}
	})
}

func TestAuthConfigRequiresEagleTokenSettings(t *testing.T) {
	bc := loadConfig(t)
	bc.Auth.Audience = ""
	bc.Auth.SigningKeyDirectory = ""
	bc.Auth.ActiveSigningKeyId = ""

	err := appconfig.Validate(bc)
	if err == nil {
		t.Fatal("未配置 Eagle token audience 和签名密钥时应拒绝启动")
	}
	message := err.Error()
	for _, want := range []string{"auth.audience is required", "auth.signing_key_directory is required", "auth.active_signing_key_id is required"} {
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

	if auth.GetAudience() == "" {
		t.Error("auth.audience 未配置")
	}
	if auth.GetSigningKeyDirectory() == "" || auth.GetActiveSigningKeyId() == "" {
		t.Error("auth signing key directory 和 active key id 必须配置")
	}
	if auth.GetAccessTokenTtl().AsDuration() <= 0 {
		t.Error("auth.access_token_ttl 必须为正数")
	}
	if auth.GetRefreshTokenTtl().AsDuration() <= auth.GetAccessTokenTtl().AsDuration() {
		t.Error("auth.refresh_token_ttl 必须大于 access_token_ttl")
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
