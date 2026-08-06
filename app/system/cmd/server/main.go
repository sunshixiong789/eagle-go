// Command server 是 eagle system 服务的入口：用户、角色、权限、字典。
package main

import (
	"flag"
	"log/slog"
	"os"
	"strings"

	"github.com/go-kratos/kratos/contrib/otel/v3/tracing"
	"github.com/go-kratos/kratos/v3"
	"github.com/go-kratos/kratos/v3/config"
	"github.com/go-kratos/kratos/v3/config/file"
	"github.com/go-kratos/kratos/v3/log"
	"github.com/go-kratos/kratos/v3/transport/grpc"
	"github.com/go-kratos/kratos/v3/transport/http"

	"github.com/eagle-go/eagle/app/system/internal/conf"

	// 按可用 CPU 配额设置 GOMAXPROCS。容器里 runtime 默认看到的是宿主机核数，
	// 不修正会导致调度器开出远超 CPU limit 的 P，引发大量无谓的上下文切换。
	_ "go.uber.org/automaxprocs"
)

// 由 -ldflags 注入，见 Makefile。
var (
	// Name 是服务名。
	Name = "eagle.system"
	// Version 是构建版本。
	Version string

	flagconf string

	id, _ = os.Hostname()
)

func init() {
	flag.StringVar(&flagconf, "conf", "../../configs", "配置文件目录或文件路径, 例如: -conf config.yaml")
}

func main() {
	flag.Parse()

	c := config.New(config.WithSource(file.NewSource(flagconf)))
	defer func() { _ = c.Close() }()

	if err := c.Load(); err != nil {
		panic(err)
	}

	var bc conf.Bootstrap
	if err := c.Scan(&bc); err != nil {
		panic(err)
	}

	logger := newLogger(bc.GetObservability())
	log.SetDefault(logger)

	app, cleanup, err := wireApp(
		bc.GetServer(),
		bc.GetData(),
		bc.GetAuth(),
		bc.GetObservability(),
		logger,
	)
	if err != nil {
		panic(err)
	}
	defer cleanup()

	if err := app.Run(); err != nil {
		panic(err)
	}
}

// newLogger 构造服务日志器。
//
// v3 的日志层就是标准库 slog（v2 的 log.Helper 已不存在）。
// WithExtractor(tracing.TraceAttrs) 会把当前 span 的 trace_id/span_id
// 自动注入每条日志，日志和链路因此可以直接关联，无需手工透传。
func newLogger(o *conf.Observability) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     parseLevel(o.GetLogLevel()),
	})

	return log.NewLogger(handler, log.WithExtractor(tracing.TraceAttrs)).With(
		slog.String("service.id", id),
		slog.String("service.name", Name),
		slog.String("service.version", Version),
	)
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func newApp(logger *slog.Logger, gs *grpc.Server, hs *http.Server) *kratos.App {
	return kratos.New(
		kratos.ID(id),
		kratos.Name(Name),
		kratos.Version(Version),
		kratos.Metadata(map[string]string{}),
		kratos.Logger(logger),
		kratos.Server(gs, hs),
	)
}
