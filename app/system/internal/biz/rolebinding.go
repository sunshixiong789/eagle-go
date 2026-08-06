package biz

import (
	"context"
	"strings"
)

// RoleBinding 是一个角色及其被授予的权限码。
type RoleBinding struct {
	Role            string
	PermissionCodes []string
}

// PolicyStore 是 Casbin 策略的读写接口，由 data 层适配到具体判定器。
//
// biz 层不直接依赖 casbin 包：授权引擎属于基础设施选型，
// 日后若换成别的实现（或加数据权限的 ABAC 模型），领域层不应受影响。
type PolicyStore interface {
	// Allow 判断任一角色是否被授予了该权限码
	Allow(ctx context.Context, roles []string, perm string) (bool, error)
	// RolePermissions 返回角色被直接授予的权限码（不含继承）
	RolePermissions(ctx context.Context, role string) ([]string, error)
	// PermissionsOf 汇总若干角色的全部权限码（含继承），去重
	PermissionsOf(ctx context.Context, roles []string) ([]string, error)
	// SetRolePermissions 全量覆盖角色的权限码
	SetRolePermissions(ctx context.Context, role string, perms []string) error
	// ListBoundRoles 列出已配置过权限的角色
	ListBoundRoles(ctx context.Context) ([]string, error)
	// AddRoleInheritance 建立 child 继承 parent
	AddRoleInheritance(ctx context.Context, child, parent string) error
}

// RoleBindingUsecase 编排角色权限映射。
type RoleBindingUsecase struct {
	policy   PolicyStore
	permRepo PermissionRepo
}

// NewRoleBindingUsecase 构造用例。
func NewRoleBindingUsecase(policy PolicyStore, permRepo PermissionRepo) *RoleBindingUsecase {
	return &RoleBindingUsecase{policy: policy, permRepo: permRepo}
}

// ListBoundRoles 列出已配置权限的角色及其权限码。
func (uc *RoleBindingUsecase) ListBoundRoles(ctx context.Context) ([]*RoleBinding, error) {
	roles, err := uc.policy.ListBoundRoles(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]*RoleBinding, 0, len(roles))
	for _, role := range roles {
		perms, err := uc.policy.RolePermissions(ctx, role)
		if err != nil {
			return nil, err
		}
		out = append(out, &RoleBinding{Role: role, PermissionCodes: perms})
	}
	return out, nil
}

// GetRolePermissions 返回单个角色的权限码。
func (uc *RoleBindingUsecase) GetRolePermissions(ctx context.Context, role string) (*RoleBinding, error) {
	perms, err := uc.policy.RolePermissions(ctx, role)
	if err != nil {
		return nil, err
	}
	if len(perms) == 0 {
		return nil, ErrRoleNotBound
	}
	return &RoleBinding{Role: role, PermissionCodes: perms}, nil
}

// SetRolePermissions 全量覆盖角色的权限码。
//
// 写入前校验每个权限码都存在于权限树中：拼错的权限码不会命中任何接口，
// 但配置的人会以为授权成功了——这种静默失败比直接报错难查得多。
// 通配符（system:*）跳过校验，它本就不对应具体节点。
func (uc *RoleBindingUsecase) SetRolePermissions(ctx context.Context, role string, perms []string) error {
	known, err := uc.knownPermissionCodes(ctx)
	if err != nil {
		return err
	}

	for _, p := range perms {
		if strings.Contains(p, "*") {
			continue
		}
		if _, ok := known[p]; !ok {
			return ErrUnknownPermissionCode.WithMetadata(map[string]string{"code": p})
		}
	}

	return uc.policy.SetRolePermissions(ctx, role, perms)
}

// AddRoleInheritance 建立角色继承。
func (uc *RoleBindingUsecase) AddRoleInheritance(ctx context.Context, child, parent string) error {
	if child == parent {
		return ErrPermissionCycle
	}
	return uc.policy.AddRoleInheritance(ctx, child, parent)
}

// PermissionsOf 汇总若干角色展开后的全部权限码。
func (uc *RoleBindingUsecase) PermissionsOf(ctx context.Context, roles []string) ([]string, error) {
	return uc.policy.PermissionsOf(ctx, roles)
}

func (uc *RoleBindingUsecase) knownPermissionCodes(ctx context.Context) (map[string]struct{}, error) {
	perms, err := uc.permRepo.List(ctx, ListPermissionsQuery{})
	if err != nil {
		return nil, err
	}
	known := make(map[string]struct{}, len(perms))
	for _, p := range perms {
		if p.Code != "" {
			known[p.Code] = struct{}{}
		}
	}
	return known, nil
}
