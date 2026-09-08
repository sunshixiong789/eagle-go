package domain

import (
	"cmp"
	"context"
	"fmt"
	"slices"
)

// Role 是 Eagle 分配的稳定角色键，如 user、admin、support-agent。
type Role string

// NewRole 校验角色键以 ASCII 字母开头，后续仅含字母、数字、下划线或连字符，总长度不超过 64 字节。
func NewRole(s string) (Role, error) {
	if len(s) == 0 || len(s) > 64 {
		return "", ErrEmptyRole
	}
	for i, r := range s {
		letter := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		digit := r >= '0' && r <= '9'
		if !letter && (i == 0 || !digit && r != '-' && r != '_') {
			return "", ErrEmptyRole
		}
	}
	return Role(s), nil
}

func (r Role) String() string { return string(r) }
func (r Role) IsZero() bool   { return r == "" }

// RoleBinding 是「角色 → 权限码集合」的聚合根，对应 Casbin 的 p 策略。
//
// 用户与角色的归属由 auth 模块维护，本聚合只负责角色被授予的直接权限。
type RoleBinding struct {
	role     Role
	codes    []PermissionCode
	revision int64
}

// NewRoleBinding 构造角色权限绑定，并对权限码去重。
//
// 去重而不是报错：重复授予同一权限码不改变判定结果，
// 为它报错只会让调用方在无害的输入上受阻。
func NewRoleBinding(role Role, codes []PermissionCode) (*RoleBinding, error) {
	if role.IsZero() {
		return nil, ErrEmptyRole
	}

	seen := make(map[string]struct{}, len(codes))
	deduped := make([]PermissionCode, 0, len(codes))
	for _, c := range codes {
		if c.IsZero() {
			continue
		}
		if _, dup := seen[c.String()]; dup {
			continue
		}
		seen[c.String()] = struct{}{}
		deduped = append(deduped, c)
	}

	slices.SortFunc(deduped, func(a, b PermissionCode) int {
		return cmp.Compare(a.String(), b.String())
	})

	return &RoleBinding{role: role, codes: deduped}, nil
}

func (b *RoleBinding) Role() Role              { return b.role }
func (b *RoleBinding) Codes() []PermissionCode { return b.codes }
func (b *RoleBinding) CodeStrings() []string   { return PermissionCodeStrings(b.codes) }
func (b *RoleBinding) IsEmpty() bool           { return len(b.codes) == 0 }
func (b *RoleBinding) Revision() int64         { return b.revision }

// WithRevision 返回带权威策略版本的副本。
func (b *RoleBinding) WithRevision(revision int64) *RoleBinding {
	if b == nil {
		return nil
	}
	next := *b
	next.revision = revision
	return &next
}

// Grants 判断本绑定是否覆盖目标权限码（考虑通配）。
//
// 与 Casbin 模型里的 keyMatch 语义保持一致。领域层保留一份独立实现，
// 是为了让「授权是否生效」可以脱离 Casbin 单测；
// 两处一旦不同步，就会出现后台显示已授权、实际调用却 403。
func (b *RoleBinding) Grants(target PermissionCode) bool {
	return slices.ContainsFunc(b.codes, func(c PermissionCode) bool {
		return c.Covers(target)
	})
}

// EnsureCodesKnown 校验全部具体权限码都存在于权限目录中。
//
// 含通配的策略跳过校验——它本就不对应目录里的一条契约。
// 不做这道校验的话，拼错的权限码会静默失效：策略写进去了，
// 却永远匹配不到任何接口，而配置的人以为已经授权成功。
func (b *RoleBinding) EnsureCodesKnown(known map[string]struct{}) error {
	for _, c := range b.codes {
		if c.HasWildcard() {
			continue
		}
		if _, ok := known[c.String()]; !ok {
			return fmt.Errorf("%w: %s", ErrUnknownPermissionCode, c)
		}
	}
	return nil
}

// RoleInheritance 是角色继承关系，对应 Casbin 的 g 规则。
type RoleInheritance struct {
	Child  Role
	Parent Role
}

// NewRoleInheritance 构造继承关系并拦截自继承。
func NewRoleInheritance(child, parent Role) (RoleInheritance, error) {
	if child.IsZero() || parent.IsZero() {
		return RoleInheritance{}, ErrEmptyRole
	}
	if child.String() == parent.String() {
		return RoleInheritance{}, ErrSelfInheritance
	}
	return RoleInheritance{Child: child, Parent: parent}, nil
}

// PolicyRepo 是授权策略的仓储接口，由基础设施层适配到具体判定引擎。
//
// 写操作在同一事务内检查可选的全局 expectedVersion、修改策略、记录审计并推进版本；
// 版本不匹配返回 ErrConcurrentModification，nil 表示不检查版本。
// 写入提交后同步重载本实例；若重载失败，返回已提交版本及错误，调用方不能据此认定写入已回滚。
type PolicyRepo interface {
	// FindBinding 返回角色的直接权限及对应的全局策略版本；未绑定时返回非 nil 的空绑定。
	FindBinding(ctx context.Context, role Role) (*RoleBinding, error)
	// SaveBinding 全量覆盖直接权限，空集合清除直接授权；未登记或停用的具体权限码返回 ErrUnknownPermissionCode。
	SaveBinding(ctx context.Context, b *RoleBinding, expectedVersion *int64) (int64, error)
	// ListBindings 按角色键升序返回有直接授权的角色；列表与全局策略版本来自同一快照，包括空列表。
	ListBindings(ctx context.Context) ([]*RoleBinding, int64, error)
	// ResolveCodes 从本实例已加载的策略中汇总并去重角色的权限，展开角色继承但保留权限通配码。
	ResolveCodes(ctx context.Context, roles []Role) ([]PermissionCode, error)
	// SaveInheritance 建立继承；形成环时返回 ErrRoleInheritanceCycle，关系已存在时不推进版本。
	SaveInheritance(ctx context.Context, ri RoleInheritance, expectedVersion *int64) (int64, error)
	// ListInheritances 按子角色、父角色升序返回继承关系及同一快照的全局策略版本。
	ListInheritances(ctx context.Context) ([]RoleInheritance, int64, error)
	// DeleteInheritance 删除继承；关系不存在时成功返回当前版本，不推进版本。
	DeleteInheritance(ctx context.Context, ri RoleInheritance, expectedVersion *int64) (int64, error)
}
