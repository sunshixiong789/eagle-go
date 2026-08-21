// Command server 是 eagle 后端服务的进程入口。
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"strings"
	"time"

	kratosmetrics "github.com/go-kratos/kratos/contrib/otel/v3/metrics"
	"github.com/go-kratos/kratos/contrib/otel/v3/tracing"
	"github.com/go-kratos/kratos/v3"
	kratosconfig "github.com/go-kratos/kratos/v3/config"
	"github.com/go-kratos/kratos/v3/config/env"
	"github.com/go-kratos/kratos/v3/config/file"
	"github.com/go-kratos/kratos/v3/log"
	"github.com/go-kratos/kratos/v3/transport/grpc"
	"github.com/go-kratos/kratos/v3/transport/http"

	appconfig "github.com/eagle-go/eagle/internal/platform/config"
	"github.com/eagle-go/eagle/pkg/healthx"
	"github.com/eagle-go/eagle/pkg/otelx"

	// 按可用 CPU 配额设置 GOMAXPROCS。容器里 runtime 默认看到的是宿主机核数，
	// 不修正会导致调度器开出远超 CPU limit 的 P，引发大量无谓的上下文切换。
	_ "go.uber.org/automaxprocs"
)

// 由 -ldflags 注入，见 Makefile。
var (
	// Name 是服务名。
	Name = "eagle.server"
	// Version 是构建版本。
	Version string

	flagconf string

	id, _ = os.Hostname()
)

func init() {
	flag.StringVar(&flagconf, "conf", "configs", "配置文件目录或文件路径, 例如: -conf configs")
}

func main() {
	flag.Parse()

	c := kratosconfig.New(kratosconfig.WithSource(
		file.NewSource(flagconf),
		// EAGLE_DATABASE_DSN 等变量会替换配置文件中的 ${DATABASE_DSN:默认值}。
		env.NewSource("EAGLE"),
	))
	defer func() { _ = c.Close() }()

	if err := c.Load(); err != nil {
		panic(err)
	}

	var bc appconfig.Bootstrap
	if err := c.Scan(&bc); err != nil {
		panic(err)
	}
	if err := appconfig.Validate(&bc); err != nil {
		panic(err)
	}

	logger := newLogger(bc.GetObservability())
	log.SetDefault(logger)

	// 可观测性要在装配业务组件之前初始化：中间件在构造时就会
	// 从全局 provider 取 Tracer/Meter，晚于它初始化会拿到 no-op 实例
	shutdownOtel, err := setupObservability(bc.GetObservability())
	if err != nil {
		panic(err)
	}
	defer func() {
		// 给缓冲区里的 span 一点时间上报，否则进程一退就全丢了
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownOtel(ctx); err != nil {
			logger.Warn("关闭可观测性组件时出错", slog.Any("error", err))
		}
	}()

	app, cleanup, err := buildApp(
		bc.GetServer(),
		bc.GetData(),
		bc.GetAuth(),
		bc.GetFile(),
		logger,
	)
	if err != nil {
		panic(err)
	}
	defer cleanup()
	healthx.Default.MarkInitialized()

	if err := app.Run(); err != nil {
		panic(err)
	}
}

// setupObservability 初始化链路与指标。
//
// 未配置 OTLP 端点时不报错、只降级：本地开发通常没有 collector，
// 为此让服务起不来是本末倒置。
func setupObservability(o *appconfig.Observability) (func(context.Context) error, error) {
	return otelx.Setup(context.Background(), otelx.Config{
		ServiceName:    Name,
		ServiceVersion: Version,
		InstanceID:     id,
		OTLPEndpoint:   o.GetOtlpEndpoint(),
		OTLPInsecure:   o.GetOtlpInsecure(),
		SampleRatio:    o.GetTraceSampleRatio(),
		MetricsAddr:    o.GetMetricsAddr(),
		HistogramViews: []string{kratosmetrics.DefaultServerSecondsHistogramName},
	})
}

// newLogger 构造服务日志器。
//
// v3 的日志层就是标准库 slog（v2 的 log.Helper 已不存在）。
// WithExtractor(tracing.TraceAttrs) 会把当前 span 的 trace_id/span_id
// 自动注入每条日志，日志和链路因此可以直接关联，无需手工透传。
func newLogger(o *appconfig.Observability) *slog.Logger {
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
