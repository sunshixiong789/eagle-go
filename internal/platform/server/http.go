package server

import (
	stdhttp "net/http"

	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport/http"

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

// NewHTTPServer 构造 HTTP 服务器。
func NewHTTPServer(
	c *config.Server,
	fileConf *config.File,
	ms []middleware.Middleware,
	perm *accessinterfaces.PermissionService,
	dict *dictionaryinterfaces.DictService,
	binding *accessinterfaces.RoleBindingService,
	files *fileinterfaces.FileService,
	notifications *notificationinterfaces.NotificationService,
) *http.Server {
	opts := []http.ServerOption{
		http.Middleware(ms...),
		http.Filter(fileUploadLimitFilter(fileConf.GetMaxSizeBytes())),
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

	accessv1.RegisterPermissionServiceHTTPServer(srv, perm)
	dictionaryv1.RegisterDictServiceHTTPServer(srv, dict)
	accessv1.RegisterRoleBindingServiceHTTPServer(srv, binding)
	filev1.RegisterFileServiceHTTPServer(srv, files)
	notificationv1.RegisterNotificationServiceHTTPServer(srv, notifications)

	return srv
}

func fileUploadLimitFilter(maxBytes int64) http.FilterFunc {
	return func(next stdhttp.Handler) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			if r.Method == stdhttp.MethodPost && r.URL.Path == "/v1/system/files:upload" {
				r.Body = stdhttp.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}
