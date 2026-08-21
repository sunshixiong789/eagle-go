package server

import (
	"context"
	"errors"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/middleware"

	accessv1 "github.com/eagle-go/eagle/api/eagle/access/v1"
	dictionaryv1 "github.com/eagle-go/eagle/api/eagle/dictionary/v1"
	filev1 "github.com/eagle-go/eagle/api/eagle/file/v1"
	notificationv1 "github.com/eagle-go/eagle/api/eagle/notification/v1"
	accessdomain "github.com/eagle-go/eagle/internal/modules/access/domain"
	dictionarydomain "github.com/eagle-go/eagle/internal/modules/dictionary/domain"
	filedomain "github.com/eagle-go/eagle/internal/modules/file/domain"
	notificationdomain "github.com/eagle-go/eagle/internal/modules/notification/domain"
)

var errorMapping = []struct {
	domainErr error
	toKratos  func(error) *kerrors.Error
}{
	{accessdomain.ErrPermissionNotFound, notFound(accessv1.ErrorReason_ERROR_REASON_PERMISSION_NOT_FOUND)},
	{accessdomain.ErrPermissionCodeDuplicated, conflict(accessv1.ErrorReason_ERROR_REASON_PERMISSION_CODE_DUPLICATED)},
	{accessdomain.ErrPermissionHasChildren, conflict(accessv1.ErrorReason_ERROR_REASON_PERMISSION_HAS_CHILDREN)},
	{accessdomain.ErrPermissionCycle, badRequest(accessv1.ErrorReason_ERROR_REASON_PERMISSION_CYCLE)},
	{accessdomain.ErrConcurrentModification, conflict(accessv1.ErrorReason_ERROR_REASON_CONCURRENT_MODIFICATION)},
	{accessdomain.ErrInvalidPermissionCode, badRequest(accessv1.ErrorReason_ERROR_REASON_INVALID_PERMISSION_CODE)},
	{accessdomain.ErrButtonRequiresCode, badRequest(accessv1.ErrorReason_ERROR_REASON_BUTTON_REQUIRES_CODE)},
	{accessdomain.ErrInvalidPermissionType, badRequest(accessv1.ErrorReason_ERROR_REASON_INVALID_PERMISSION_TYPE)},
	{accessdomain.ErrEmptyPermissionName, badRequest(accessv1.ErrorReason_ERROR_REASON_EMPTY_PERMISSION_NAME)},
	{accessdomain.ErrRoleNotBound, notFound(accessv1.ErrorReason_ERROR_REASON_ROLE_NOT_BOUND)},
	{accessdomain.ErrUnknownPermissionCode, badRequest(accessv1.ErrorReason_ERROR_REASON_UNKNOWN_PERMISSION_CODE)},
	{accessdomain.ErrEmptyRole, badRequest(accessv1.ErrorReason_ERROR_REASON_EMPTY_ROLE)},
	{accessdomain.ErrSelfInheritance, badRequest(accessv1.ErrorReason_ERROR_REASON_ROLE_INHERITANCE_CYCLE)},
	{accessdomain.ErrRoleInheritanceCycle, badRequest(accessv1.ErrorReason_ERROR_REASON_ROLE_INHERITANCE_CYCLE)},
	{dictionarydomain.ErrDictTypeNotFound, notFound(dictionaryv1.ErrorReason_ERROR_REASON_DICT_TYPE_NOT_FOUND)},
	{dictionarydomain.ErrDictTypeDuplicated, conflict(dictionaryv1.ErrorReason_ERROR_REASON_DICT_TYPE_DUPLICATED)},
	{dictionarydomain.ErrDictDataNotFound, notFound(dictionaryv1.ErrorReason_ERROR_REASON_DICT_DATA_NOT_FOUND)},
	{dictionarydomain.ErrDictDataDuplicated, conflict(dictionaryv1.ErrorReason_ERROR_REASON_DICT_DATA_DUPLICATED)},
	{filedomain.ErrFileNotFound, notFound(filev1.ErrorReason_ERROR_REASON_FILE_NOT_FOUND)},
	{filedomain.ErrFileTooLarge, badRequest(filev1.ErrorReason_ERROR_REASON_FILE_TOO_LARGE)},
	{filedomain.ErrInvalidFileName, badRequest(filev1.ErrorReason_ERROR_REASON_INVALID_FILE_NAME)},
	{filedomain.ErrInvalidContentType, badRequest(filev1.ErrorReason_ERROR_REASON_INVALID_FILE_CONTENT_TYPE)},
	{notificationdomain.ErrNotificationNotFound, notFound(notificationv1.ErrorReason_ERROR_REASON_NOTIFICATION_NOT_FOUND)},
}

// ErrorMapping 把领域/应用错误统一翻译成传输错误。application 因而不再依赖
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

type errorReason interface{ String() string }

func notFound(r errorReason) func(error) *kerrors.Error {
	return func(err error) *kerrors.Error { return kerrors.NotFound(r.String(), err.Error()) }
}

func conflict(r errorReason) func(error) *kerrors.Error {
	return func(err error) *kerrors.Error { return kerrors.Conflict(r.String(), err.Error()) }
}

func badRequest(r errorReason) func(error) *kerrors.Error {
	return func(err error) *kerrors.Error { return kerrors.BadRequest(r.String(), err.Error()) }
}
