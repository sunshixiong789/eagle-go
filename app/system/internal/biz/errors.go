package biz

import (
	"github.com/go-kratos/kratos/v3/errors"

	v1 "github.com/eagle-go/eagle/api/eagle/system/v1"
)

// 领域错误。构造方式沿用官方 kratos-layout：
// HTTP/gRPC 状态码由构造函数决定，reason 取自 proto 枚举，
// 客户端按 reason 分支处理而不是去匹配错误文案。
var (
	ErrPermissionNotFound       = errors.NotFound(reason(v1.ErrorReason_ERROR_REASON_PERMISSION_NOT_FOUND), "权限不存在")
	ErrPermissionCodeDuplicated = errors.Conflict(reason(v1.ErrorReason_ERROR_REASON_PERMISSION_CODE_DUPLICATED), "权限码已存在")
	ErrPermissionHasChildren    = errors.Conflict(reason(v1.ErrorReason_ERROR_REASON_PERMISSION_HAS_CHILDREN), "存在子节点，无法删除")
	ErrPermissionCycle          = errors.BadRequest(reason(v1.ErrorReason_ERROR_REASON_PERMISSION_CYCLE), "上级权限不能是自身或其后代")

	ErrDictTypeNotFound   = errors.NotFound(reason(v1.ErrorReason_ERROR_REASON_DICT_TYPE_NOT_FOUND), "字典类型不存在")
	ErrDictTypeDuplicated = errors.Conflict(reason(v1.ErrorReason_ERROR_REASON_DICT_TYPE_DUPLICATED), "字典类型已存在")
	ErrDictDataNotFound   = errors.NotFound(reason(v1.ErrorReason_ERROR_REASON_DICT_DATA_NOT_FOUND), "字典项不存在")
	ErrDictDataDuplicated = errors.Conflict(reason(v1.ErrorReason_ERROR_REASON_DICT_DATA_DUPLICATED), "同一字典下键值已存在")

	ErrRoleNotBound = errors.NotFound(reason(v1.ErrorReason_ERROR_REASON_ROLE_NOT_BOUND), "该角色尚未配置任何权限")
	// 授予权限树中不存在的权限码通常是拼写错误。放过去的话，
	// 这条策略永远不会命中任何接口，而配置的人会以为已经授权成功。
	ErrUnknownPermissionCode = errors.BadRequest(reason(v1.ErrorReason_ERROR_REASON_UNKNOWN_PERMISSION_CODE), "权限码在权限树中不存在")
)

func reason(r v1.ErrorReason) string { return r.String() }
