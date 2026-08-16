// Package domain 是纯领域层：实体（含不变量）、值对象、领域服务、
// 仓储接口与领域事件。
//
// 本包不 import 任何基础设施（ent / redis / casbin / protobuf），
// 也不 import 应用层。依赖方向自外向内单向流动：
//
//	server -> service -> biz(用例) -> domain
//	                      data ────────┘（实现 domain 定义的接口）
//
// 于是领域规则可以完全脱离数据库和传输层做单元测试。
package domain

import "errors"

// 领域错误。刻意用标准库 error 而非 Kratos 的 errors：
// HTTP/gRPC 状态码属于传输层关注点，领域层不该知道 404 和 409 的存在。
// 状态码到错误的映射在 biz/service 边界上完成。
var (
	// ErrInvalidPermissionCode 表示权限码格式不合法。
	ErrInvalidPermissionCode = errors.New("domain: 权限码格式不合法")

	// ErrPermissionNotFound 表示权限节点不存在。
	ErrPermissionNotFound = errors.New("domain: 权限不存在")
	// ErrPermissionCodeDuplicated 表示权限码已被占用。
	ErrPermissionCodeDuplicated = errors.New("domain: 权限码已存在")
	// ErrPermissionHasChildren 表示节点仍有子节点，不能删除。
	ErrPermissionHasChildren = errors.New("domain: 存在子节点，无法删除")
	// ErrPermissionCycle 表示父子关系变更会形成环。
	ErrPermissionCycle = errors.New("domain: 上级权限不能是自身或其后代")
	// ErrConcurrentModification 表示调用方基于过期版本写入。
	ErrConcurrentModification = errors.New("domain: 数据已被其他操作更新，请刷新后重试")
	// ErrButtonRequiresCode 表示按钮型节点缺少权限码。
	ErrButtonRequiresCode = errors.New("domain: 按钮类权限必须声明权限码")
	// ErrInvalidPermissionType 表示节点类型取值越界。
	ErrInvalidPermissionType = errors.New("domain: 权限类型不合法")
	// ErrEmptyPermissionName 表示节点名为空。
	ErrEmptyPermissionName = errors.New("domain: 权限名称不能为空")

	// ErrEmptyRole 表示角色名为空。
	ErrEmptyRole = errors.New("domain: 角色名不能为空")
	// ErrRoleNotBound 表示角色尚未配置任何权限。
	ErrRoleNotBound = errors.New("domain: 该角色尚未配置任何权限")
	// ErrUnknownPermissionCode 表示授予了权限树中不存在的权限码。
	ErrUnknownPermissionCode = errors.New("domain: 权限码在权限树中不存在")
	// ErrSelfInheritance 表示角色继承自身。
	ErrSelfInheritance      = errors.New("domain: 角色不能继承自身")
	ErrRoleInheritanceCycle = errors.New("domain: 角色继承关系不能形成环")

	// ErrDictTypeNotFound 表示字典类型不存在。
	ErrDictTypeNotFound = errors.New("domain: 字典类型不存在")
	// ErrDictTypeDuplicated 表示字典类型已存在。
	ErrDictTypeDuplicated = errors.New("domain: 字典类型已存在")
	// ErrDictDataNotFound 表示字典项不存在。
	ErrDictDataNotFound = errors.New("domain: 字典项不存在")
	// ErrDictDataDuplicated 表示同一字典下键值重复。
	ErrDictDataDuplicated = errors.New("domain: 同一字典下键值已存在")
)
