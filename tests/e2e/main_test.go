// Package e2e 端到端验证真实服务：真实 PostgreSQL、真实迁移、
// 真实 HTTP 服务器、真实签名的 JWT，走完整的中间件链。
//
// 与各模块 infrastructure 集成测试的区别：那里验证仓储实现与 SQL，
// 这里验证的是「服务作为一个整体能不能起来并正确响应」——
// 配置解析、依赖装配、中间件顺序、认证与授权判定、错误码映射。
//
// 不依赖 Docker：PostgreSQL 由 embedded-postgres 在进程内拉起，
// Keycloak 用一个签发真实 RS256 token 的替身。
package e2e

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	accessapp "github.com/eagle-go/eagle/internal/modules/access/application"
	"github.com/eagle-go/eagle/internal/modules/access/domain"
	accessinfra "github.com/eagle-go/eagle/internal/modules/access/infrastructure"
	accessinterfaces "github.com/eagle-go/eagle/internal/modules/access/interfaces"
	dictionaryapp "github.com/eagle-go/eagle/internal/modules/dictionary/application"
	dictionaryinfra "github.com/eagle-go/eagle/internal/modules/dictionary/infrastructure"
	dictionaryinterfaces "github.com/eagle-go/eagle/internal/modules/dictionary/interfaces"
	fileapp "github.com/eagle-go/eagle/internal/modules/file/application"
	fileinfra "github.com/eagle-go/eagle/internal/modules/file/infrastructure"
	fileinterfaces "github.com/eagle-go/eagle/internal/modules/file/interfaces"
	notificationapp "github.com/eagle-go/eagle/internal/modules/notification/application"
	notificationinfra "github.com/eagle-go/eagle/internal/modules/notification/infrastructure"
	notificationinterfaces "github.com/eagle-go/eagle/internal/modules/notification/interfaces"
	"github.com/eagle-go/eagle/internal/platform/config"
	platformdb "github.com/eagle-go/eagle/internal/platform/database"
	"github.com/eagle-go/eagle/internal/platform/server"
	"github.com/eagle-go/eagle/pkg/authz"
	"github.com/eagle-go/eagle/pkg/identity"
	"github.com/eagle-go/eagle/tests/testkit"
)

const (
	clientID  = "eagle-system"
	adminRole = "admin"
)

var testDSN string

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}

	pg, err := testkit.StartPostgres("e2e", "eagle_e2e")
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e 环境准备失败: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = pg.Close() }()
	testDSN = pg.DSN
	os.Exit(m.Run())
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

	dataConf := &config.Data{
		Database: &config.Data_Database{Dsn: testDSN, MaxConns: 4, MaxIdleConns: 1},
	}
	authConf := &config.Auth{
		Issuer:         kc.issuer(),
		ClientId:       clientID,
		Audience:       clientID,
		SuperAdminRole: adminRole,
	}

	db, cleanup, err := platformdb.Open(dataConf)
	if err != nil {
		t.Fatalf("构造 Data: %v", err)
	}
	t.Cleanup(cleanup)

	store := accessinfra.NewPolicyStore(db)

	enforcer, err := accessinfra.NewEnforcer(store)
	if err != nil {
		t.Fatalf("构造 Casbin enforcer: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	verifier := server.NewVerifier(authConf)
	middlewares, err := server.NewMiddlewares(logger, verifier, enforcer, authConf)
	if err != nil {
		t.Fatalf("构造中间件链: %v", err)
	}

	t.Cleanup(accessinfra.NewPolicyReconciler(store, enforcer, logger))

	permRepo := accessinfra.NewPermissionRepo(db)
	policyRepo := accessinfra.NewPolicyRepo(enforcer, store)
	dictRepo := dictionaryinfra.NewDictRepo(db)
	fileRepo := fileinfra.NewRepository(db)
	notificationRepo := notificationinfra.NewRepository(db)
	blobs, err := fileinfra.NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("构造本地文件存储: %v", err)
	}

	permSvc := accessinterfaces.NewPermissionService(accessapp.NewPermissionUsecase(permRepo, policyRepo))
	dictSvc := dictionaryinterfaces.NewDictService(dictionaryapp.NewDictUsecase(dictRepo))
	bindingSvc := accessinterfaces.NewRoleBindingService(accessapp.NewRoleBindingUsecase(policyRepo, permRepo))
	fileSvc := fileinterfaces.NewFileService(fileapp.NewUsecase(fileRepo, blobs, 10<<20))
	notificationSvc := notificationinterfaces.NewNotificationService(notificationapp.NewUsecase(notificationRepo))

	// addr 留空：不监听真实端口，只把 Server 当 http.Handler 用
	srv := server.NewHTTPServer(&config.Server{
		Http: &config.Server_HTTP{Timeout: durationpb.New(10 * time.Second)},
	}, &config.File{MaxSizeBytes: 10 << 20}, middlewares, permSvc, dictSvc, bindingSvc, fileSvc, notificationSvc)

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
