package service

import (
	"context"
	"errors"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/middleware"

	v1 "github.com/eagle-go/eagle/api/eagle/system/v1"
	"github.com/eagle-go/eagle/app/system/internal/domain"
)

var errorMapping = []struct {
	domainErr error
	toKratos  func(error) *kerrors.Error
}{
	{domain.ErrPermissionNotFound, notFound(v1.ErrorReason_ERROR_REASON_PERMISSION_NOT_FOUND)},
	{domain.ErrPermissionCodeDuplicated, conflict(v1.ErrorReason_ERROR_REASON_PERMISSION_CODE_DUPLICATED)},
	{domain.ErrPermissionHasChildren, conflict(v1.ErrorReason_ERROR_REASON_PERMISSION_HAS_CHILDREN)},
	{domain.ErrPermissionCycle, badRequest(v1.ErrorReason_ERROR_REASON_PERMISSION_CYCLE)},
	{domain.ErrConcurrentModification, conflict(v1.ErrorReason_ERROR_REASON_CONCURRENT_MODIFICATION)},
	{domain.ErrInvalidPermissionCode, badRequest(v1.ErrorReason_ERROR_REASON_INVALID_PERMISSION_CODE)},
	{domain.ErrButtonRequiresCode, badRequest(v1.ErrorReason_ERROR_REASON_BUTTON_REQUIRES_CODE)},
	{domain.ErrInvalidPermissionType, badRequest(v1.ErrorReason_ERROR_REASON_INVALID_PERMISSION_TYPE)},
	{domain.ErrEmptyPermissionName, badRequest(v1.ErrorReason_ERROR_REASON_EMPTY_PERMISSION_NAME)},
	{domain.ErrRoleNotBound, notFound(v1.ErrorReason_ERROR_REASON_ROLE_NOT_BOUND)},
	{domain.ErrUnknownPermissionCode, badRequest(v1.ErrorReason_ERROR_REASON_UNKNOWN_PERMISSION_CODE)},
	{domain.ErrEmptyRole, badRequest(v1.ErrorReason_ERROR_REASON_EMPTY_ROLE)},
	{domain.ErrSelfInheritance, badRequest(v1.ErrorReason_ERROR_REASON_ROLE_INHERITANCE_CYCLE)},
	{domain.ErrRoleInheritanceCycle, badRequest(v1.ErrorReason_ERROR_REASON_ROLE_INHERITANCE_CYCLE)},
	{domain.ErrDictTypeNotFound, notFound(v1.ErrorReason_ERROR_REASON_DICT_TYPE_NOT_FOUND)},
	{domain.ErrDictTypeDuplicated, conflict(v1.ErrorReason_ERROR_REASON_DICT_TYPE_DUPLICATED)},
	{domain.ErrDictDataNotFound, notFound(v1.ErrorReason_ERROR_REASON_DICT_DATA_NOT_FOUND)},
	{domain.ErrDictDataDuplicated, conflict(v1.ErrorReason_ERROR_REASON_DICT_DATA_DUPLICATED)},
}

// ErrorMapping 把领域/应用错误统一翻译成传输错误。biz 因而不再依赖
// protobuf 或 Kratos，可被其它传输协议直接复用。
func ErrorMapping() middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			resp, err := handler(ctx, req)
			return resp, toTransportError(err)
		}
	}
}

func toTransportError(err error) error {
	if err == nil {
		return nil
	}
	for _, m := range errorMapping {
		if errors.Is(err, m.domainErr) {
			return m.toKratos(err).WithCause(err)
		}
	}
	return err
}

func notFound(r v1.ErrorReason) func(error) *kerrors.Error {
	return func(err error) *kerrors.Error { return kerrors.NotFound(r.String(), err.Error()) }
}

func conflict(r v1.ErrorReason) func(error) *kerrors.Error {
	return func(err error) *kerrors.Error { return kerrors.Conflict(r.String(), err.Error()) }
}

func badRequest(r v1.ErrorReason) func(error) *kerrors.Error {
	return func(err error) *kerrors.Error { return kerrors.BadRequest(r.String(), err.Error()) }
}
