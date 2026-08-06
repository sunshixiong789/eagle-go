package biz

import (
	"github.com/go-kratos/kratos/v3/errors"

	v1 "github.com/eagle-go/eagle/api/eagle/system/v1"
)

// 领域错误。构造方式沿用官方 kratos-layout：
// HTTP/gRPC 状态码由构造函数决定，reason 取自 proto 枚举，
// 客户端按 reason 分支处理而不是去匹配错误文案。
var (
	ErrUserNotFound      = errors.NotFound(reason(v1.ErrorReason_ERROR_REASON_USER_NOT_FOUND), "用户不存在")
	ErrUserAlreadyExists = errors.Conflict(reason(v1.ErrorReason_ERROR_REASON_USER_ALREADY_EXISTS), "用户名已被占用")
	ErrUserDisabled      = errors.Forbidden(reason(v1.ErrorReason_ERROR_REASON_USER_DISABLED), "账号已停用")
	// 对外不区分"用户不存在"与"密码错误"，避免账号枚举
	ErrPasswordIncorrect = errors.Unauthorized(reason(v1.ErrorReason_ERROR_REASON_PASSWORD_INCORRECT), "用户名或密码错误")
	ErrUserProtected     = errors.Forbidden(reason(v1.ErrorReason_ERROR_REASON_USER_PROTECTED), "内置管理员不允许删除或停用")

	ErrRoleNotFound       = errors.NotFound(reason(v1.ErrorReason_ERROR_REASON_ROLE_NOT_FOUND), "角色不存在")
	ErrRoleCodeDuplicated = errors.Conflict(reason(v1.ErrorReason_ERROR_REASON_ROLE_CODE_DUPLICATED), "角色码已存在")
	ErrRoleInUse          = errors.Conflict(reason(v1.ErrorReason_ERROR_REASON_ROLE_IN_USE), "该角色下仍有用户，无法删除")
	ErrRoleProtected      = errors.Forbidden(reason(v1.ErrorReason_ERROR_REASON_ROLE_PROTECTED), "内置角色不允许删除")

	ErrPermissionNotFound       = errors.NotFound(reason(v1.ErrorReason_ERROR_REASON_PERMISSION_NOT_FOUND), "权限不存在")
	ErrPermissionCodeDuplicated = errors.Conflict(reason(v1.ErrorReason_ERROR_REASON_PERMISSION_CODE_DUPLICATED), "权限码已存在")
	ErrPermissionHasChildren    = errors.Conflict(reason(v1.ErrorReason_ERROR_REASON_PERMISSION_HAS_CHILDREN), "存在子节点，无法删除")
	ErrPermissionCycle          = errors.BadRequest(reason(v1.ErrorReason_ERROR_REASON_PERMISSION_CYCLE), "上级权限不能是自身或其后代")

	ErrDictTypeNotFound   = errors.NotFound(reason(v1.ErrorReason_ERROR_REASON_DICT_TYPE_NOT_FOUND), "字典类型不存在")
	ErrDictTypeDuplicated = errors.Conflict(reason(v1.ErrorReason_ERROR_REASON_DICT_TYPE_DUPLICATED), "字典类型已存在")
	ErrDictDataNotFound   = errors.NotFound(reason(v1.ErrorReason_ERROR_REASON_DICT_DATA_NOT_FOUND), "字典项不存在")
	ErrDictDataDuplicated = errors.Conflict(reason(v1.ErrorReason_ERROR_REASON_DICT_DATA_DUPLICATED), "同一字典下键值已存在")
)

func reason(r v1.ErrorReason) string { return r.String() }
