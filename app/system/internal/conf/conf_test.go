package conf_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v3/config"
	"github.com/go-kratos/kratos/v3/config/file"

	"github.com/eagle-go/eagle/app/system/internal/conf"
)

// 配置文件里的字段名拼错不会导致编译失败——proto 解析时对不上的键
// 会被静默丢弃，直到运行时发现某个功能"没配置"才暴露。
// 这个测试真实加载 configs/config.yaml，把这类错误提前到 CI。
func loadConfig(t *testing.T) *conf.Bootstrap {
	t.Helper()

	path, err := filepath.Abs(filepath.Join("..", "..", "configs"))
	if err != nil {
		t.Fatalf("解析配置目录: %v", err)
	}

	c := config.New(config.WithSource(file.NewSource(path)))
	t.Cleanup(func() { _ = c.Close() })

	if err := c.Load(); err != nil {
		t.Fatalf("加载配置: %v", err)
	}

	var bc conf.Bootstrap
	if err := c.Scan(&bc); err != nil {
		t.Fatalf("解析配置到 proto: %v", err)
	}
	return &bc
}

func TestConfigParses(t *testing.T) {
	bc := loadConfig(t)

	if bc.GetServer().GetHttp().GetAddr() == "" {
		t.Error("server.http.addr 未解析出来")
	}
	if bc.GetServer().GetGrpc().GetAddr() == "" {
		t.Error("server.grpc.addr 未解析出来")
	}
	if bc.GetData().GetDatabase().GetDsn() == "" {
		t.Error("data.database.dsn 未解析出来")
	}
	if bc.GetData().GetRedis().GetAddr() == "" {
		t.Error("data.redis.addr 未解析出来")
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
	// 指标端点必须与业务端口分开：它不经过认证鉴权中间件，
	// 与业务共用端口就等于给业务服务开了个免鉴权的口子
	bc := loadConfig(t)
	if o.GetMetricsAddr() == bc.GetServer().GetHttp().GetAddr() {
		t.Error("metrics_addr 与业务 HTTP 端口相同，指标端点会绕过鉴权暴露业务接口")
	}
}

// 缓存 TTL 为零会导致缓存立即过期，退化成每次请求都打库。
func TestCacheTTLIsPositive(t *testing.T) {
	ttl := loadConfig(t).GetAuth().GetDictCacheTtl().AsDuration()
	if ttl <= 0 {
		t.Errorf("dict_cache_ttl = %v, 应为正值否则缓存形同虚设", ttl)
	}
	if ttl > 24*time.Hour {
		t.Errorf("dict_cache_ttl = %v 过长，字典变更后失效不及时", ttl)
	}
}
