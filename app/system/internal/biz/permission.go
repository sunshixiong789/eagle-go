package biz

import (
	"context"

	"github.com/eagle-go/eagle/app/system/internal/domain"
)

// PermissionUsecase 是权限模块的应用服务。
//
// 应用层只做编排：取聚合、调用领域行为、落库。
// 业务规则（防环、按钮必须有权限码、菜单可见性）全部在 domain 里，
// 这里不出现任何 if 判断业务条件的代码。
type PermissionUsecase struct {
	repo   domain.PermissionRepo
	policy domain.PolicyRepo
}

// NewPermissionUsecase 构造权限用例。
func NewPermissionUsecase(repo domain.PermissionRepo, policy domain.PolicyRepo) *PermissionUsecase {
	return &PermissionUsecase{repo: repo, policy: policy}
}

// CreatePermission 新建权限节点。
func (uc *PermissionUsecase) CreatePermission(ctx context.Context, params domain.NewPermissionParams) (*domain.Permission, error) {
	// 构造即校验：不变量在实体里，这里拿到的一定是合法节点
	perm, err := domain.NewPermission(params)
	if err != nil {
		return nil, toTransportError(err)
	}

	if !perm.IsRoot() {
		if _, err := uc.repo.GetByID(ctx, perm.ParentID()); err != nil {
			return nil, toTransportError(err)
		}
	}

	created, err := uc.repo.Create(ctx, perm)
	return created, toTransportError(err)
}

// GetPermission 按 ID 取权限节点。
func (uc *PermissionUsecase) GetPermission(ctx context.Context, id int64) (*domain.Permission, error) {
	p, err := uc.repo.GetByID(ctx, id)
	return p, toTransportError(err)
}

// ListPermissions 返回平铺的权限列表。
func (uc *PermissionUsecase) ListPermissions(ctx context.Context, q domain.ListPermissionsQuery) ([]*domain.Permission, error) {
	perms, err := uc.repo.List(ctx, q)
	return perms, toTransportError(err)
}

// UpdatePermission 更新权限节点。
//
// 变更父节点时需要纵观全树才能判断是否成环，
// 因此先把整棵树取出来交给 PermissionTree 判定——
// 权限总量只有百级，一次全量查询的代价可以忽略。
func (uc *PermissionUsecase) UpdatePermission(ctx context.Context, id int64, params domain.NewPermissionParams) (*domain.Permission, error) {
	current, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return nil, toTransportError(err)
	}

	if params.ParentID != current.ParentID() {
		tree, err := uc.loadTree(ctx)
		if err != nil {
			return nil, toTransportError(err)
		}
		if err := tree.EnsureNoCycle(id, params.ParentID); err != nil {
			return nil, toTransportError(err)
		}
	}

	if err := current.Update(params); err != nil {
		return nil, toTransportError(err)
	}

	updated, err := uc.repo.Update(ctx, current)
	return updated, toTransportError(err)
}

// DeletePermission 删除权限节点。
func (uc *PermissionUsecase) DeletePermission(ctx context.Context, id int64) error {
	perm, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return toTransportError(err)
	}

	childCount, err := uc.repo.CountChildren(ctx, id)
	if err != nil {
		return toTransportError(err)
	}
	if err := perm.EnsureDeletable(childCount); err != nil {
		return toTransportError(err)
	}

	return toTransportError(uc.repo.Delete(ctx, id))
}

// GetMenusForRoles 返回给定角色可见的菜单与全部权限码。
//
// 菜单与鉴权判定共用同一份 Casbin 策略，因此不会出现
// 「菜单看得见但点了 403」这种前后端权限口径不一致的情况。
func (uc *PermissionUsecase) GetMenusForRoles(ctx context.Context, roleNames []string) ([]*domain.Permission, []domain.PermissionCode, error) {
	roles, err := toRoles(roleNames)
	if err != nil {
		return nil, nil, toTransportError(err)
	}

	codes, err := uc.policy.ResolveCodes(ctx, roles)
	if err != nil {
		return nil, nil, toTransportError(err)
	}

	tree, err := uc.loadTree(ctx)
	if err != nil {
		return nil, nil, toTransportError(err)
	}

	return tree.VisibleMenus(codes), codes, nil
}

func (uc *PermissionUsecase) loadTree(ctx context.Context) (*domain.PermissionTree, error) {
	perms, err := uc.repo.List(ctx, domain.ListPermissionsQuery{})
	if err != nil {
		return nil, err
	}
	return domain.NewPermissionTree(perms), nil
}

// toRoles 把字符串角色名批量转成值对象，跳过空串。
func toRoles(names []string) ([]domain.Role, error) {
	out := make([]domain.Role, 0, len(names))
	for _, n := range names {
		if n == "" {
			continue
		}
		r, err := domain.NewRole(n)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
