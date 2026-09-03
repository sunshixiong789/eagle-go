package server

import (
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport/http"

	"github.com/eagle-go/eagle/pkg/platform/config"
)

// HTTPRegistrar lets each process register only the APIs it owns.
type HTTPRegistrar func(*http.Server)

func NewHTTPServer(
	c *config.Server,
	ms []middleware.Middleware,
	registrars ...HTTPRegistrar,
) *http.Server {
	opts := []http.ServerOption{http.Middleware(ms...)}
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
	for _, register := range registrars {
		register(srv)
	}
	return srv
}
