package server

import (
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport/grpc"

	accessv1 "github.com/eagle-go/eagle/api/eagle/access/v1"
	dictionaryv1 "github.com/eagle-go/eagle/api/eagle/dictionary/v1"
	filev1 "github.com/eagle-go/eagle/api/eagle/file/v1"
	notificationv1 "github.com/eagle-go/eagle/api/eagle/notification/v1"
	accessinterfaces "github.com/eagle-go/eagle/internal/modules/access/interfaces"
	dictionaryinterfaces "github.com/eagle-go/eagle/internal/modules/dictionary/interfaces"
	fileinterfaces "github.com/eagle-go/eagle/internal/modules/file/interfaces"
	notificationinterfaces "github.com/eagle-go/eagle/internal/modules/notification/interfaces"
	"github.com/eagle-go/eagle/internal/platform/config"
)

// NewGRPCServer 构造 gRPC 服务器。
func NewGRPCServer(
	c *config.Server,
	ms []middleware.Middleware,
	perm *accessinterfaces.PermissionService,
	dict *dictionaryinterfaces.DictService,
	binding *accessinterfaces.RoleBindingService,
	files *fileinterfaces.FileService,
	notifications *notificationinterfaces.NotificationService,
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

	accessv1.RegisterPermissionServiceServer(srv, perm)
	dictionaryv1.RegisterDictServiceServer(srv, dict)
	accessv1.RegisterRoleBindingServiceServer(srv, binding)
	filev1.RegisterFileServiceServer(srv, files)
	notificationv1.RegisterNotificationServiceServer(srv, notifications)

	return srv
}
