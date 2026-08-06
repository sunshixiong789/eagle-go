package biz

import (
	"context"

	"github.com/eagle-go/eagle/app/system/internal/domain"
)

// RoleBindingUsecase 是角色权限绑定的应用服务。
type RoleBindingUsecase struct {
	policy   domain.PolicyRepo
	permRepo domain.PermissionRepo
}

// NewRoleBindingUsecase 构造用例。
func NewRoleBindingUsecase(policy domain.PolicyRepo, permRepo domain.PermissionRepo) *RoleBindingUsecase {
	return &RoleBindingUsecase{policy: policy, permRepo: permRepo}
}

// ListBoundRoles 列出已配置权限的角色及其权限码。
func (uc *RoleBindingUsecase) ListBoundRoles(ctx context.Context) ([]*domain.RoleBinding, error) {
	roles, err := uc.policy.ListBoundRoles(ctx)
	if err != nil {
		return nil, toTransportError(err)
	}

	out := make([]*domain.RoleBinding, 0, len(roles))
	for _, role := range roles {
		b, err := uc.policy.FindBinding(ctx, role)
		if err != nil {
			return nil, toTransportError(err)
		}
		out = append(out, b)
	}
	return out, nil
}

// GetRolePermissions 返回单个角色的权限绑定。
func (uc *RoleBindingUsecase) GetRolePermissions(ctx context.Context, roleName string) (*domain.RoleBinding, error) {
	role, err := domain.NewRole(roleName)
	if err != nil {
		return nil, toTransportError(err)
	}

	b, err := uc.policy.FindBinding(ctx, role)
	if err != nil {
		return nil, toTransportError(err)
	}
	if b.IsEmpty() {
		return nil, toTransportError(domain.ErrRoleNotBound)
	}
	return b, nil
}

// SetRolePermissions 全量覆盖角色的权限码。
//
// 写入前校验每个权限码都存在于权限树中：拼错的权限码不会命中任何接口，
// 但配置的人会以为授权成功了——这种静默失败比直接报错难查得多。
// 这是一条跨聚合的规则（RoleBinding 需要 Permission 树的知识），
// 因此由应用层负责把两个聚合的数据凑齐，判定逻辑仍在领域对象上。
func (uc *RoleBindingUsecase) SetRolePermissions(ctx context.Context, roleName string, codeStrings []string) error {
	role, err := domain.NewRole(roleName)
	if err != nil {
		return toTransportError(err)
	}

	codes, err := domain.ParsePermissionCodes(codeStrings)
	if err != nil {
		return toTransportError(err)
	}

	binding, err := domain.NewRoleBinding(role, codes)
	if err != nil {
		return toTransportError(err)
	}

	perms, err := uc.permRepo.List(ctx, domain.ListPermissionsQuery{})
	if err != nil {
		return toTransportError(err)
	}
	tree := domain.NewPermissionTree(perms)

	if err := binding.EnsureCodesKnown(tree.KnownCodes()); err != nil {
		return toTransportError(err)
	}

	return toTransportError(uc.policy.SaveBinding(ctx, binding))
}

// AddRoleInheritance 建立角色继承。
func (uc *RoleBindingUsecase) AddRoleInheritance(ctx context.Context, childName, parentName string) error {
	child, err := domain.NewRole(childName)
	if err != nil {
		return toTransportError(err)
	}
	parent, err := domain.NewRole(parentName)
	if err != nil {
		return toTransportError(err)
	}

	ri, err := domain.NewRoleInheritance(child, parent)
	if err != nil {
		return toTransportError(err)
	}
	return toTransportError(uc.policy.SaveInheritance(ctx, ri))
}

// ResolveCodes 汇总若干角色展开继承后的全部权限码。
func (uc *RoleBindingUsecase) ResolveCodes(ctx context.Context, roleNames []string) ([]domain.PermissionCode, error) {
	roles, err := toRoles(roleNames)
	if err != nil {
		return nil, toTransportError(err)
	}
	codes, err := uc.policy.ResolveCodes(ctx, roles)
	return codes, toTransportError(err)
}
