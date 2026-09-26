package server

import (
	"context"
	"errors"
	"net/http"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/middleware"
	khttp "github.com/go-kratos/kratos/v3/transport/http"
)

// errorReason 提供传输错误使用的稳定原因码，通常由 Proto 错误枚举实现。
type errorReason interface {
	// String 返回客户端可用于分支判断的原因码，不返回本地化错误消息。
	String() string
}

// ErrorMappingRule keeps transport error ownership in each service composition
// root while the conversion mechanism remains shared.
type ErrorMappingRule struct {
	domainErr error
	toKratos  func(error) *kerrors.Error
}

func NotFound(domainErr error, reason errorReason) ErrorMappingRule {
	return ErrorMappingRule{domainErr: domainErr, toKratos: func(error) *kerrors.Error {
		return kerrors.NotFound(reason.String(), domainErr.Error())
	}}
}

func Conflict(domainErr error, reason errorReason) ErrorMappingRule {
	return ErrorMappingRule{domainErr: domainErr, toKratos: func(error) *kerrors.Error {
		return kerrors.Conflict(reason.String(), domainErr.Error())
	}}
}

func BadRequest(domainErr error, reason errorReason) ErrorMappingRule {
	return ErrorMappingRule{domainErr: domainErr, toKratos: func(error) *kerrors.Error {
		return kerrors.BadRequest(reason.String(), domainErr.Error())
	}}
}

func Unauthorized(domainErr error, reason errorReason) ErrorMappingRule {
	return ErrorMappingRule{domainErr: domainErr, toKratos: func(error) *kerrors.Error {
		return kerrors.Unauthorized(reason.String(), domainErr.Error())
	}}
}

func Forbidden(domainErr error, reason errorReason) ErrorMappingRule {
	return ErrorMappingRule{domainErr: domainErr, toKratos: func(error) *kerrors.Error {
		return kerrors.Forbidden(reason.String(), domainErr.Error())
	}}
}

func ErrorMapping(rules ...ErrorMappingRule) middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			resp, err := handler(ctx, req)
			return resp, toTransportError(err, rules)
		}
	}
}

func toTransportError(err error, rules []ErrorMappingRule) error {
	if err == nil {
		return nil
	}
	for _, rule := range rules {
		if errors.Is(err, rule.domainErr) {
			return rule.toKratos(err).WithCause(err)
		}
	}
	return publicError(err)
}

// publicError 保留明确的客户端错误；服务端错误只暴露稳定原因码，原始信息留在 cause 中供日志记录。
func publicError(err error) error {
	if err == nil || kerrors.Code(err) < http.StatusInternalServerError {
		return err
	}
	return kerrors.New(kerrors.Code(err), "INTERNAL_ERROR", "服务暂时不可用").WithCause(err)
}

// encodeError 也覆盖鉴权、恢复和协议解码等未经过业务错误映射的响应。
func encodeError(w http.ResponseWriter, r *http.Request, err error) {
	khttp.DefaultErrorEncoder(w, r, publicError(err))
}
