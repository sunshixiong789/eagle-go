// Package e2e 端到端验证真实服务：真实 PostgreSQL、真实迁移、
// 真实 HTTP 服务器、真实签名的 JWT，走完整的中间件链。
//
// 与 data 包的集成测试的区别：那里验证的是仓储实现与 SQL，
// 这里验证的是「服务作为一个整体能不能起来并正确响应」——
// 配置解析、依赖装配、中间件顺序、认证与授权判定、错误码映射。
//
// 不依赖 Docker：PostgreSQL 由 embedded-postgres 在进程内拉起，
// Keycloak 用一个签发真实 RS256 token 的替身。
package e2e

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	_ "github.com/lib/pq"
	"github.com/pressly/goose/v3"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/eagle-go/eagle/app/system/internal/biz"
	"github.com/eagle-go/eagle/app/system/internal/conf"
	"github.com/eagle-go/eagle/app/system/internal/data"
	"github.com/eagle-go/eagle/app/system/internal/domain"
	"github.com/eagle-go/eagle/app/system/internal/server"
	"github.com/eagle-go/eagle/app/system/internal/service"
	"github.com/eagle-go/eagle/pkg/authz"
	"github.com/eagle-go/eagle/pkg/identity"
)

const (
	clientID  = "eagle-system"
	adminRole = "admin"
)

var (
	testPG  *embeddedpostgres.EmbeddedPostgres
	testDSN string
)

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}

	code, err := run(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e 环境准备失败: %v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func run(m *testing.M) (int, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, fmt.Errorf("定位用户目录: %w", err)
	}
	// 独立于其他测试包的运行目录：go test ./... 并行执行不同包，
	// 共用解压目录会互相踩（详见 data 包 main_test.go 的说明）
	runtimeDir := filepath.Join(home, ".embedded-postgres-go", "eagle-e2e")
	dataDir, err := os.MkdirTemp("", "eagle-e2e-postgres-")
	if err != nil {
		return 0, fmt.Errorf("创建 PostgreSQL 临时数据目录: %w", err)
	}
	defer func() { _ = os.RemoveAll(dataDir) }()
	port, err := availablePort()
	if err != nil {
		return 0, err
	}

	testPG = embeddedpostgres.NewDatabase(
		embeddedpostgres.DefaultConfig().
			Username("eagle").Password("eagle").Database("eagle_e2e").
			Port(port).
			RuntimePath(runtimeDir).
			DataPath(dataDir).
			Logger(io.Discard),
	)
	if err := testPG.Start(); err != nil {
		return 0, fmt.Errorf("启动 embedded postgres: %w", err)
	}
	defer func() { _ = testPG.Stop() }()

	testDSN = fmt.Sprintf(
		"postgres://eagle:eagle@127.0.0.1:%d/eagle_e2e?sslmode=disable", port)

	if err := migrate(testDSN); err != nil {
		return 0, err
	}

	return m.Run(), nil
}

func availablePort() (uint32, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("分配 PostgreSQL 测试端口: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return 0, fmt.Errorf("释放 PostgreSQL 测试端口: %w", err)
	}
	return uint32(port), nil
}

func migrate(dsn string) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	goose.SetLogger(goose.NopLogger())

	root, err := repoRoot()
	if err != nil {
		return err
	}
	if err := goose.Up(db, filepath.Join(root, "db", "migrations")); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

// repoRoot 向上查找 go.mod 来定位仓库根。
//
// 不写死 ../../.. 这类相对路径：包一旦挪动位置，写死的层数就会指向
// 不存在的目录，而报错信息（「目录不存在」）完全不提示真正的原因。
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("获取工作目录: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("从工作目录向上未找到 go.mod，无法定位仓库根")
		}
		dir = parent
	}
}

// testEnv 是一次测试用的完整服务实例。
type testEnv struct {
	http     *httptest.Server
	kc       *fakeKeycloak
	enforcer *authz.Enforcer
	policy   domain.PolicyRepo
}

// newTestEnv 用与生产完全相同的构造函数装配服务。
//
// 刻意不走 buildApp：那个函数返回 *kratos.App，会真正监听端口。
// 这里逐个调用同样的 provider，把 http.Server 交给 httptest——
// 装配路径与生产一致，但不占用固定端口，测试可以并行。
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	if testing.Short() {
		t.Skip("需要真实数据库")
	}

	kc := newFakeKeycloak(t)

	dataConf := &conf.Data{
		Database: &conf.Data_Database{Dsn: testDSN, MaxConns: 4, MaxIdleConns: 1},
	}
	authConf := &conf.Auth{
		Issuer:         kc.issuer(),
		ClientId:       clientID,
		Audience:       clientID,
		SuperAdminRole: adminRole,
	}

	d, cleanup, err := data.NewData(dataConf)
	if err != nil {
		t.Fatalf("构造 Data: %v", err)
	}
	t.Cleanup(cleanup)

	entClient := data.NewEntClient(d)
	store := data.NewPolicyStore(entClient)

	enforcer, err := data.NewEnforcer(store)
	if err != nil {
		t.Fatalf("构造 Casbin enforcer: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	verifier := server.NewVerifier(authConf)
	middlewares, err := server.NewMiddlewares(logger, verifier, enforcer, authConf)
	if err != nil {
		t.Fatalf("构造中间件链: %v", err)
	}

	t.Cleanup(data.NewPolicyReconciler(store, enforcer, logger))

	permRepo := data.NewPermissionRepo(d)
	policyRepo := data.NewPolicyRepo(enforcer, store)
	dictRepo := data.NewDictRepo(d)

	permSvc := service.NewPermissionService(biz.NewPermissionUsecase(permRepo, policyRepo))
	dictSvc := service.NewDictService(biz.NewDictUsecase(dictRepo))
	bindingSvc := service.NewRoleBindingService(biz.NewRoleBindingUsecase(policyRepo, permRepo))

	// addr 留空：不监听真实端口，只把 Server 当 http.Handler 用
	srv := server.NewHTTPServer(&conf.Server{
		Http: &conf.Server_HTTP{Timeout: durationpb.New(10 * time.Second)},
	}, middlewares, permSvc, dictSvc, bindingSvc)

	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	return &testEnv{http: ts, kc: kc, enforcer: enforcer, policy: policyRepo}
}

// grantRole 给角色授予权限码，并让判定器立即生效。
func (e *testEnv) grantRole(t *testing.T, role string, perms ...string) {
	t.Helper()
	if !identity.ValidRoleKey(role) {
		role = identity.RealmRoleKey(role)
	}
	roleValue, err := domain.NewRole(role)
	if err != nil {
		t.Fatalf("构造角色 %s: %v", role, err)
	}
	codes, err := domain.ParsePermissionCodes(perms)
	if err != nil {
		t.Fatalf("解析权限码: %v", err)
	}
	binding, err := domain.NewRoleBinding(roleValue, codes)
	if err != nil {
		t.Fatalf("构造角色绑定: %v", err)
	}
	if _, err := e.policy.SaveBinding(context.Background(), binding, nil); err != nil {
		t.Fatalf("授予角色 %s 权限: %v", role, err)
	}
}
