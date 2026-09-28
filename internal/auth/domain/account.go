package domain

import (
	"context"
	"errors"
	"slices"
)

var (
	ErrAccountNotFound         = errors.New("auth: 账号不存在")
	ErrInvalidRoleAssignment   = errors.New("auth: 角色分配参数无效")
	ErrRoleRevisionConflict    = errors.New("auth: 账号角色已变更，请重新读取")
	ErrLastAdmin               = errors.New("auth: 不能撤销最后一个启用管理员")
	ErrAdminAlreadyInitialized = errors.New("auth: 当前应用已初始化管理员")
)

type Account struct {
	Subject, DisplayName string
	Status               int32
}
type AccountQuery struct {
	Keyword string
	Offset  int64
	Limit   int
}
type AccountRoles struct {
	Roles    []string
	Revision int64
}
type RoleAssignment struct {
	Subject          string
	Roles            []string
	ExpectedRevision int64
	Actor            string
}

// NewRoleAssignment 校验角色键、版本与审计主体，去重排序并复制调用方切片。
func NewRoleAssignment(subject string, roles []string, revision int64, actor string) (RoleAssignment, error) {
	if subject == "" || len(subject) > 32 || actor == "" || len(actor) > 128 || revision <= 0 || len(roles) > 32 {
		return RoleAssignment{}, ErrInvalidRoleAssignment
	}
	for _, role := range roles {
		if len(role) == 0 || len(role) > 64 {
			return RoleAssignment{}, ErrInvalidRoleAssignment
		}
		for i, r := range role {
			letter := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
			digit := r >= '0' && r <= '9'
			if !letter && (i == 0 || !digit && r != '-' && r != '_') {
				return RoleAssignment{}, ErrInvalidRoleAssignment
			}
		}
	}
	normalized := slices.Clone(roles)
	slices.Sort(normalized)
	return RoleAssignment{Subject: subject, Roles: slices.Compact(normalized), ExpectedRevision: revision, Actor: actor}, nil
}

// AccountRepository 维护配置 audience 内的角色、并发版本和不可分割的变更审计。
type AccountRepository interface {
	// List 按 subject 升序分页查询账号，总数与列表不保证处于同一快照。
	List(context.Context, AccountQuery) ([]Account, int64, error)
	// Roles 读取账号角色和 audience 全局版本；账号不存在返回 ErrAccountNotFound。
	Roles(context.Context, string) (*AccountRoles, error)
	// Replace 在同一事务内检查版本及最后管理员约束，替换角色并记录审计；空角色不会在登录时恢复。
	Replace(context.Context, RoleAssignment) (int64, error)
	// BootstrapAdmin 为已存在且启用的账号追加 admin，仅允许每个 audience 成功一次，并记录操作人。
	// 并发调用只有一次成功；已有管理员或已初始化返回 ErrAdminAlreadyInitialized。
	BootstrapAdmin(context.Context, string, string) error
}
