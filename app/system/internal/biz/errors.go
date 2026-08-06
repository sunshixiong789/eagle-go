package biz

import (
	"errors"

	kerrors "github.com/go-kratos/kratos/v3/errors"

	v1 "github.com/eagle-go/eagle/api/eagle/system/v1"
	"github.com/eagle-go/eagle/app/system/internal/domain"
)

// 领域层用标准库 error 表达业务错误，不知道 HTTP 状态码的存在。
// 状态码是传输层关注点，映射在这条边界上完成——领域规则因此可以
// 脱离 Kratos 单测，换传输框架也不必改领域代码。
var errorMapping = []struct {
	domainErr error
	toKratos  func(error) *kerrors.Error
}{
	{domain.ErrPermissionNotFound, notFound(v1.ErrorReason_ERROR_REASON_PERMISSION_NOT_FOUND)},
	{domain.ErrPermissionCodeDuplicated, conflict(v1.ErrorReason_ERROR_REASON_PERMISSION_CODE_DUPLICATED)},
	{domain.ErrPermissionHasChildren, conflict(v1.ErrorReason_ERROR_REASON_PERMISSION_HAS_CHILDREN)},
	{domain.ErrPermissionCycle, badRequest(v1.ErrorReason_ERROR_REASON_PERMISSION_CYCLE)},

	// 以下都是「请求本身不合法」，统一映射成 400
	{domain.ErrInvalidPermissionCode, badRequest(v1.ErrorReason_ERROR_REASON_UNKNOWN_PERMISSION_CODE)},
	{domain.ErrButtonRequiresCode, badRequest(v1.ErrorReason_ERROR_REASON_UNKNOWN_PERMISSION_CODE)},
	{domain.ErrInvalidPermissionType, badRequest(v1.ErrorReason_ERROR_REASON_UNKNOWN_PERMISSION_CODE)},
	{domain.ErrEmptyPermissionName, badRequest(v1.ErrorReason_ERROR_REASON_UNKNOWN_PERMISSION_CODE)},

	{domain.ErrRoleNotBound, notFound(v1.ErrorReason_ERROR_REASON_ROLE_NOT_BOUND)},
	{domain.ErrUnknownPermissionCode, badRequest(v1.ErrorReason_ERROR_REASON_UNKNOWN_PERMISSION_CODE)},
	{domain.ErrEmptyRole, badRequest(v1.ErrorReason_ERROR_REASON_ROLE_NOT_BOUND)},
	{domain.ErrSelfInheritance, badRequest(v1.ErrorReason_ERROR_REASON_PERMISSION_CYCLE)},

	{domain.ErrDictTypeNotFound, notFound(v1.ErrorReason_ERROR_REASON_DICT_TYPE_NOT_FOUND)},
	{domain.ErrDictTypeDuplicated, conflict(v1.ErrorReason_ERROR_REASON_DICT_TYPE_DUPLICATED)},
	{domain.ErrDictDataNotFound, notFound(v1.ErrorReason_ERROR_REASON_DICT_DATA_NOT_FOUND)},
	{domain.ErrDictDataDuplicated, conflict(v1.ErrorReason_ERROR_REASON_DICT_DATA_DUPLICATED)},
}

// toTransportError 把领域错误翻译成带状态码的 Kratos 错误。
//
// 无法识别的错误原样返回而不是包成 500：那多半是基础设施故障
// （数据库超时之类），已经在 data 层带上了足够的上下文，
// 在这里再裹一层只会丢失原因。
func toTransportError(err error) error {
	if err == nil {
		return nil
	}
	for _, m := range errorMapping {
		if errors.Is(err, m.domainErr) {
			// 保留原始错误作为 cause，日志里仍能看到领域层的具体描述
			return m.toKratos(err).WithCause(err)
		}
	}
	return err
}

func notFound(r v1.ErrorReason) func(error) *kerrors.Error {
	return func(err error) *kerrors.Error {
		return kerrors.NotFound(r.String(), err.Error())
	}
}

func conflict(r v1.ErrorReason) func(error) *kerrors.Error {
	return func(err error) *kerrors.Error {
		return kerrors.Conflict(r.String(), err.Error())
	}
}

func badRequest(r v1.ErrorReason) func(error) *kerrors.Error {
	return func(err error) *kerrors.Error {
		return kerrors.BadRequest(r.String(), err.Error())
	}
}
