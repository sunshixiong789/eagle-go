//go:build wireinject
// +build wireinject

// 本文件仅供 wire 读取以生成 wire_gen.go，不参与正常构建。
package main

import (
	"log/slog"

	"github.com/go-kratos/kratos/v3"
	"github.com/google/wire"

	"github.com/eagle-go/eagle/app/system/internal/biz"
	"github.com/eagle-go/eagle/app/system/internal/conf"
	"github.com/eagle-go/eagle/app/system/internal/data"
	"github.com/eagle-go/eagle/app/system/internal/server"
	"github.com/eagle-go/eagle/app/system/internal/service"
)

// wireApp 装配 system 服务的完整依赖图。
func wireApp(
	*conf.Server,
	*conf.Data,
	*conf.Auth,
	*conf.Observability,
	*slog.Logger,
) (*kratos.App, func(), error) {
	panic(wire.Build(
		server.ProviderSet,
		data.ProviderSet,
		biz.ProviderSet,
		service.ProviderSet,
		newApp,
	))
}
