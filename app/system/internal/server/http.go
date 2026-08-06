package server

import (
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport/http"

	v1 "github.com/eagle-go/eagle/api/eagle/system/v1"
	"github.com/eagle-go/eagle/app/system/internal/conf"
	"github.com/eagle-go/eagle/app/system/internal/service"
)

// NewHTTPServer 构造 HTTP 服务器。
func NewHTTPServer(
	c *conf.Server,
	ms []middleware.Middleware,
	user *service.UserService,
	role *service.RoleService,
	perm *service.PermissionService,
	dict *service.DictService,
) *http.Server {
	opts := []http.ServerOption{
		http.Middleware(ms...),
	}
	if n := c.GetHttp().GetNetwork(); n != "" {
		opts = append(opts, http.Network(n))
	}
	if addr := c.GetHttp().GetAddr(); addr != "" {
		opts = append(opts, http.Address(addr))
	}
	if t := c.GetHttp().GetTimeout(); t != nil {
		opts = append(opts, http.Timeout(t.AsDuration()))
	}

	srv := http.NewServer(opts...)

	v1.RegisterUserServiceHTTPServer(srv, user)
	v1.RegisterRoleServiceHTTPServer(srv, role)
	v1.RegisterPermissionServiceHTTPServer(srv, perm)
	v1.RegisterDictServiceHTTPServer(srv, dict)

	return srv
}
