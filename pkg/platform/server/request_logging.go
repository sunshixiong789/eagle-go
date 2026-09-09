package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	kratoserrors "github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"
)

// logRedacter 允许请求类型提供适合写入日志的脱敏摘要。
type logRedacter interface {
	// Redact 返回脱敏后的请求摘要，不得暴露凭证或其他敏感字段原文。
	Redact() string
}

// RequestLogging 记录服务端请求，并按 HTTP 语义区分日志级别。
// 请求内容默认隐藏，仅允许请求类型显式提供脱敏摘要。
// 401/403 是正常的认证授权决策，不应污染服务端错误告警。
func RequestLogging(logger *slog.Logger) middleware.Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			startedAt := time.Now()
			reply, err := handler(ctx, req)

			code, reason := responseStatus(err)
			attrs := []slog.Attr{
				slog.String("kind", "server"),
				slog.String("component", transportKind(ctx)),
				slog.String("operation", transportOperation(ctx)),
				slog.String("args", requestArgs(req)),
				slog.Int("code", code),
				slog.String("reason", reason),
				slog.Float64("latency", time.Since(startedAt).Seconds()),
			}
			if err != nil {
				attrs = append(attrs, slog.Any("error", err))
			}
			level := requestLogLevel(code, err)
			if err != nil && level >= slog.LevelError {
				attrs = append(attrs, slog.String("stack", fmt.Sprintf("%+v", err)))
			}
			logger.LogAttrs(ctx, level, "server request", attrs...)
			return reply, err
		}
	}
}

func responseStatus(err error) (int, string) {
	if err == nil {
		return 200, ""
	}
	se := kratoserrors.FromError(err)
	return int(se.Code), se.Reason
}

func requestLogLevel(code int, err error) slog.Level {
	if err == nil || code == 401 || code == 403 {
		return slog.LevelInfo
	}
	if code >= 500 {
		return slog.LevelError
	}
	return slog.LevelWarn
}

func transportKind(ctx context.Context) string {
	if tr, ok := transport.FromServerContext(ctx); ok {
		return tr.Kind().String()
	}
	return ""
}

func transportOperation(ctx context.Context) string {
	if tr, ok := transport.FromServerContext(ctx); ok {
		return tr.Operation()
	}
	return ""
}

func requestArgs(req any) string {
	if redacter, ok := req.(logRedacter); ok {
		return redacter.Redact()
	}
	return "[REDACTED]"
}

// recoveryLogHandler 仅供 recovery 的专用 logger 使用，拦截框架附加的原始请求。
// 普通请求日志和 panic 日志沿用同一脱敏规则。
type recoveryLogHandler struct{ slog.Handler }

func (h recoveryLogHandler) Handle(ctx context.Context, record slog.Record) error {
	redacted := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "request" {
			attr = slog.String("request", requestArgs(attr.Value.Any()))
		}
		redacted.AddAttrs(attr)
		return true
	})
	return h.Handler.Handle(ctx, redacted)
}
