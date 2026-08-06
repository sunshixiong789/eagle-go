package authz

import (
	"context"
	"errors"
	"testing"

	kratoserrors "github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/transport"

	_ "github.com/eagle-go/eagle/api/eagle/system/v1"
	"github.com/eagle-go/eagle/pkg/identity"
)

// stubTransport 实现 transport.Transporter，只为让中间件取到 operation。
type stubTransport struct {
	operation string
}

func (s *stubTransport) Kind() transport.Kind            { return transport.KindGRPC }
func (s *stubTransport) Endpoint() string                { return "" }
func (s *stubTransport) Operation() string               { return s.operation }
func (s *stubTransport) RequestHeader() transport.Header { return nil }
func (s *stubTransport) ReplyHeader() transport.Header   { return nil }

func serverCtx(operation string) context.Context {
	return transport.NewServerContext(context.Background(), &stubTransport{operation: operation})
}

// 一个记录是否被调用过的终端 handler
func probeHandler(called *bool) func(context.Context, any) (any, error) {
	return func(_ context.Context, _ any) (any, error) {
		*called = true
		return "ok", nil
	}
}

func TestServerRejectsAnonymous(t *testing.T) {
	var called bool
	mw := Server(WithPermissionLoader(func(context.Context, int64) ([]string, error) {
		return nil, nil
	}))

	_, err := mw(probeHandler(&called))(serverCtx("/eagle.system.v1.UserService/CreateUser"), nil)

	if err == nil {
		t.Fatal("匿名调用应被拒绝")
	}
	if got := kratoserrors.Code(err); got != 401 {
		t.Errorf("状态码 = %d, want 401", got)
	}
	if called {
		t.Error("handler 不应被执行")
	}
}

func TestServerRejectsMissingPermission(t *testing.T) {
	var called bool
	mw := Server(WithPermissionLoader(func(context.Context, int64) ([]string, error) {
		// 用户只有查询权限，没有新增权限
		return []string{"system:user:query", "system:user:list"}, nil
	}))

	ctx := identity.NewContext(serverCtx("/eagle.system.v1.UserService/CreateUser"), &identity.Principal{
		UserID: 42, Username: "alice",
	})
	_, err := mw(probeHandler(&called))(ctx, nil)

	if err == nil {
		t.Fatal("缺少权限码时应被拒绝")
	}
	if got := kratoserrors.Code(err); got != 403 {
		t.Errorf("状态码 = %d, want 403", got)
	}
	if called {
		t.Error("handler 不应被执行")
	}
}

func TestServerAllowsWithPermission(t *testing.T) {
	var called bool
	mw := Server(WithPermissionLoader(func(context.Context, int64) ([]string, error) {
		return []string{"system:user:add"}, nil
	}))

	ctx := identity.NewContext(serverCtx("/eagle.system.v1.UserService/CreateUser"), &identity.Principal{
		UserID: 42,
	})
	if _, err := mw(probeHandler(&called))(ctx, nil); err != nil {
		t.Fatalf("持有权限码时应放行, got %v", err)
	}
	if !called {
		t.Error("handler 应被执行")
	}
}

func TestServerSuperAdminShortCircuits(t *testing.T) {
	var called bool
	loaderCalled := false
	mw := Server(
		WithSuperAdminRole("admin"),
		WithPermissionLoader(func(context.Context, int64) ([]string, error) {
			loaderCalled = true
			return nil, nil
		}),
	)

	ctx := identity.NewContext(serverCtx("/eagle.system.v1.UserService/DeleteUser"), &identity.Principal{
		UserID: 1, RoleCodes: []string{"admin"},
	})
	if _, err := mw(probeHandler(&called))(ctx, nil); err != nil {
		t.Fatalf("超管应被放行, got %v", err)
	}
	if !called {
		t.Error("handler 应被执行")
	}
	if loaderCalled {
		t.Error("超管应短路，不该再查权限表")
	}
}

func TestServerAllowsAuthenticatedWhenNoPermDeclared(t *testing.T) {
	var called bool
	mw := Server(WithPermissionLoader(func(context.Context, int64) ([]string, error) {
		return nil, nil
	}))

	// ChangeMyPassword 未声明权限码，登录即可
	ctx := identity.NewContext(serverCtx("/eagle.system.v1.UserService/ChangeMyPassword"), &identity.Principal{
		UserID: 42,
	})
	if _, err := mw(probeHandler(&called))(ctx, nil); err != nil {
		t.Fatalf("已登录用户应可访问未声明权限码的方法, got %v", err)
	}
	if !called {
		t.Error("handler 应被执行")
	}
}

func TestServerServiceTokenUsesScopes(t *testing.T) {
	mw := Server(WithPermissionLoader(func(context.Context, int64) ([]string, error) {
		t.Error("服务令牌不应触发用户权限查询")
		return nil, nil
	}))

	op := serverCtx("/eagle.system.v1.InternalUserService/VerifyCredentials")

	t.Run("scope 匹配则放行", func(t *testing.T) {
		var called bool
		ctx := identity.NewContext(op, &identity.Principal{
			IsService: true, ClientID: "eagle-auth",
			Scopes: []string{"internal:auth:verify", "internal:auth:read"},
		})
		if _, err := mw(probeHandler(&called))(ctx, nil); err != nil {
			t.Fatalf("scope 命中时应放行, got %v", err)
		}
		if !called {
			t.Error("handler 应被执行")
		}
	})

	t.Run("scope 不匹配则 403", func(t *testing.T) {
		var called bool
		ctx := identity.NewContext(op, &identity.Principal{
			IsService: true, ClientID: "some-other-service",
			Scopes: []string{"internal:something:else"},
		})
		_, err := mw(probeHandler(&called))(ctx, nil)
		if kratoserrors.Code(err) != 403 {
			t.Errorf("状态码 = %d, want 403", kratoserrors.Code(err))
		}
		if called {
			t.Error("handler 不应被执行")
		}
	})
}

// 鉴权组件的失败方向必须是关闭的：权限查询出错时拒绝，而不是放行。
func TestServerDeniesWhenLoaderFails(t *testing.T) {
	var called bool
	mw := Server(WithPermissionLoader(func(context.Context, int64) ([]string, error) {
		return nil, errors.New("database is down")
	}))

	ctx := identity.NewContext(serverCtx("/eagle.system.v1.UserService/CreateUser"), &identity.Principal{
		UserID: 42,
	})
	_, err := mw(probeHandler(&called))(ctx, nil)

	if kratoserrors.Code(err) != 403 {
		t.Errorf("权限加载失败时状态码 = %d, want 403", kratoserrors.Code(err))
	}
	if called {
		t.Error("handler 不应被执行")
	}
}

// 未配置 loader 属于装配错误，同样必须拒绝而非放行。
func TestServerDeniesWhenLoaderMissing(t *testing.T) {
	var called bool
	mw := Server()

	ctx := identity.NewContext(serverCtx("/eagle.system.v1.UserService/CreateUser"), &identity.Principal{
		UserID: 42,
	})
	_, err := mw(probeHandler(&called))(ctx, nil)

	if kratoserrors.Code(err) != 403 {
		t.Errorf("状态码 = %d, want 403", kratoserrors.Code(err))
	}
	if called {
		t.Error("handler 不应被执行")
	}
}
