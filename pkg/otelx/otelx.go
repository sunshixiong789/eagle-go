// Package otelx 负责 OpenTelemetry 的初始化：TracerProvider、MeterProvider
// 与 Prometheus 指标端点。
//
// 只依赖朴素的 Config 结构体而不是某个服务的 conf proto，
// 使多个服务能共用同一份实现，pkg 不反向依赖 app。
package otelx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	prometheusexporter "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	metricsdk "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config 是可观测性参数。
type Config struct {
	ServiceName    string
	ServiceVersion string
	InstanceID     string

	// OTLPEndpoint 为空时不上报链路，只保留本地指标。
	// 开发环境常常没有 collector，这时不该让服务起不来。
	OTLPEndpoint string
	// OTLPInsecure 为 true 时用明文 gRPC 连接 collector。
	// 集群内通常如此；跨网络传输务必置 false 并配置 TLS。
	OTLPInsecure bool

	// SampleRatio 取值 0.0~1.0。生产建议 0.01~0.1。
	SampleRatio float64

	// MetricsAddr 是 Prometheus 抓取端点地址，如 0.0.0.0:9100。留空则不暴露。
	MetricsAddr string
}

// Setup 初始化全局 TracerProvider 与 MeterProvider，并按需启动指标端点。
//
// 返回的 shutdown 需要在进程退出时调用，否则缓冲区里的 span 会丢失。
func Setup(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error) {
	res, err := buildResource(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// 收集各组件的关闭函数，任一初始化失败时回滚已完成的部分，
	// 避免留下已启动但无人管理的后台 goroutine
	var shutdowns []func(context.Context) error
	rollback := func() {
		for _, fn := range shutdowns {
			_ = fn(context.Background())
		}
	}

	tracerShutdown, err := setupTracing(ctx, cfg, res)
	if err != nil {
		rollback()
		return nil, err
	}
	if tracerShutdown != nil {
		shutdowns = append(shutdowns, tracerShutdown)
	}

	meterShutdown, err := setupMetrics(cfg, res)
	if err != nil {
		rollback()
		return nil, err
	}
	if meterShutdown != nil {
		shutdowns = append(shutdowns, meterShutdown)
	}

	if serverShutdown := startMetricsServer(cfg.MetricsAddr); serverShutdown != nil {
		shutdowns = append(shutdowns, serverShutdown)
	}

	return func(ctx context.Context) error {
		var errs []error
		// 逆序关闭：先停指标端点再停 provider，
		// 否则关停期间抓取会打到已释放的 provider 上
		for i := len(shutdowns) - 1; i >= 0; i-- {
			if err := shutdowns[i](ctx); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}, nil
}

func buildResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			semconv.ServiceInstanceID(cfg.InstanceID),
		),
	)
	if err != nil {
		// resource.New 在探测器部分失败时会同时返回可用的 res 和错误。
		// 这类错误（比如容器里读不到某些系统信息）不该阻止服务启动。
		if res == nil {
			return nil, fmt.Errorf("otelx: 构建 resource: %w", err)
		}
	}
	return res, nil
}

func setupTracing(ctx context.Context, cfg Config, res *resource.Resource) (func(context.Context) error, error) {
	// 传播器始终注册：即便本服务不上报链路，也要把上游传来的
	// traceparent 继续往下游传，否则整条链路会在这里断开
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if cfg.OTLPEndpoint == "" {
		// 开发环境常常没有 collector。此时不装 TracerProvider，
		// otel 会退化成 no-op，服务照常运行
		return nil, nil
	}

	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		// 不阻塞启动：collector 暂时不可用时导出器会自行重试，
		// 而不是让服务卡在启动阶段
		otlptracegrpc.WithTimeout(5 * time.Second),
	}
	if cfg.OTLPInsecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("otelx: 创建 OTLP trace 导出器: %w", err)
	}

	tp := tracesdk.NewTracerProvider(
		tracesdk.WithResource(res),
		tracesdk.WithBatcher(exporter),
		// ParentBased 保证同一条链路的采样决策一致：
		// 上游采了本服务就采，不会出现断断续续的半截链路
		tracesdk.WithSampler(tracesdk.ParentBased(
			tracesdk.TraceIDRatioBased(clampRatio(cfg.SampleRatio)),
		)),
	)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}

func setupMetrics(cfg Config, res *resource.Resource) (func(context.Context) error, error) {
	if cfg.MetricsAddr == "" {
		return nil, nil
	}

	exporter, err := prometheusexporter.New(
		prometheusexporter.WithRegisterer(prometheus.DefaultRegisterer),
	)
	if err != nil {
		return nil, fmt.Errorf("otelx: 创建 Prometheus 导出器: %w", err)
	}

	mp := metricsdk.NewMeterProvider(
		metricsdk.WithResource(res),
		metricsdk.WithReader(exporter),
	)
	otel.SetMeterProvider(mp)

	return mp.Shutdown, nil
}

// startMetricsServer 启动独立的指标端点。
//
// 单开一个端口而不是挂在业务 HTTP 服务上：指标端点不应经过认证鉴权
// 中间件（Prometheus 不会带 token），挂在业务服务上就得为它开一个
// 免鉴权的口子，那个口子迟早会被别的东西复用。
func startMetricsServer(addr string) func(context.Context) error {
	if addr == "" {
		return nil
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	// 存活探针放在这里，同样不该走鉴权
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			// 指标端点起不来不该拖垮业务，记录后继续
			otel.Handle(fmt.Errorf("otelx: 指标端点退出: %w", err))
		}
	}()

	return srv.Shutdown
}

// clampRatio 把采样率收敛到 [0, 1]。
// 配置写错时用边界值而不是报错——采样率不该成为服务起不来的理由。
func clampRatio(r float64) float64 {
	switch {
	case r < 0:
		return 0
	case r > 1:
		return 1
	default:
		return r
	}
}

// 业务侧要自定义指标时直接用 otel.Meter("...")：全局 MeterProvider
// 已在 Setup 里装好。这里不再包一层——包装除了多一个间接层没有任何收益。
