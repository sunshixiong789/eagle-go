// Package server 装配 HTTP / gRPC 服务器与中间件链。
package server

import (
	"context"
	"log/slog"

	protovalidatemw "github.com/go-kratos/kratos/contrib/middleware/validate/v3"
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/middleware/logging"
	"github.com/go-kratos/kratos/v3/middleware/ratelimit"
	"github.com/go-kratos/kratos/v3/middleware/recovery"
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"

	"github.com/eagle-go/eagle/app/system/internal/biz"
	"github.com/eagle-go/eagle/app/system/internal/conf"
	"github.com/eagle-go/eagle/pkg/authn"
	"github.com/eagle-go/eagle/pkg/authz"
)

// ProviderSet 是 server 层的 wire provider 集合。
var ProviderSet = wire.NewSet(
	NewGRPCServer,
	NewHTTPServer,
	NewVerifier,
	NewMiddlewares,
)

// NewVerifier 构造 token 验证器。
func NewVerifier(c *conf.Auth, rdb *redis.Client) *authn.Verifier {
	return authn.NewVerifier(
		context.Background(),
		authn.Config{Issuer: c.GetIssuer()},
		authn.NewRedisRevocations(rdb),
	)
}

// NewMiddlewares 构造 HTTP 与 gRPC 共用的中间件链。
//
// 顺序是有讲究的：
//   - recovery 必须在最外层，否则内层 panic 会直接打挂进程
//   - logging 紧随其后，才能记录到后面被拒绝的请求
//   - ratelimit 在鉴权之前：过载保护不该依赖"先把 token 验完"
//   - authn -> authz：先确认你是谁，再判断你能不能做
//   - 参数校验放最内层：只有通过鉴权的请求才值得花这个代价
func NewMiddlewares(
	logger *slog.Logger,
	verifier *authn.Verifier,
	userUC *biz.UserUsecase,
	authConf *conf.Auth,
) []middleware.Middleware {
	superAdmin := authConf.GetSuperAdminRole()
	if superAdmin == "" {
		superAdmin = biz.BuiltinAdminRoleCode
	}

	return []middleware.Middleware{
		recovery.Recovery(),
		logging.Server(logger),
		ratelimit.Server(),
		authn.Server(verifier),
		authz.Server(
			authz.WithSuperAdminRole(superAdmin),
			authz.WithPermissionLoader(userUC.ListPermissionCodes),
		),
		protovalidatemw.ProtoValidate(),
	}
}
