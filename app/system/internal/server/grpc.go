package server

import (
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport/grpc"

	v1 "github.com/eagle-go/eagle/api/eagle/system/v1"
	"github.com/eagle-go/eagle/app/system/internal/conf"
	"github.com/eagle-go/eagle/app/system/internal/service"
)

// NewGRPCServer 构造 gRPC 服务器。
//
// 内部服务（InternalUserService）只注册在这里、不绑 HTTP 路由，
// 从传输层就杜绝了它被外部直接调用的可能。
func NewGRPCServer(
	c *conf.Server,
	ms []middleware.Middleware,
	user *service.UserService,
	role *service.RoleService,
	perm *service.PermissionService,
	dict *service.DictService,
	internal *service.InternalUserService,
) *grpc.Server {
	opts := []grpc.ServerOption{
		grpc.Middleware(ms...),
	}
	if n := c.GetGrpc().GetNetwork(); n != "" {
		opts = append(opts, grpc.Network(n))
	}
	if addr := c.GetGrpc().GetAddr(); addr != "" {
		opts = append(opts, grpc.Address(addr))
	}
	if t := c.GetGrpc().GetTimeout(); t != nil {
		opts = append(opts, grpc.Timeout(t.AsDuration()))
	}

	srv := grpc.NewServer(opts...)

	v1.RegisterUserServiceServer(srv, user)
	v1.RegisterRoleServiceServer(srv, role)
	v1.RegisterPermissionServiceServer(srv, perm)
	v1.RegisterDictServiceServer(srv, dict)
	v1.RegisterInternalUserServiceServer(srv, internal)

	return srv
}
