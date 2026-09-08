package application

import (
	"context"

	"github.com/eagle-go/eagle/internal/access/domain"
)

// PermissionUsecase 编排权限用例：构造/变更实体后交给仓储落库。
// 父节点、环和子节点检查由 infrastructure 在持有树锁的事务内完成，避免检查与写入之间状态变化。
type PermissionUsecase struct {
	repo   domain.PermissionRepo
	policy domain.PolicyRepo
}

func NewPermissionUsecase(repo domain.PermissionRepo, policy domain.PolicyRepo) *PermissionUsecase {
	return &PermissionUsecase{repo: repo, policy: policy}
}

// CreatePermission 构造并校验节点的业务不变量，再交给仓储检查树约束并保存。
func (uc *PermissionUsecase) CreatePermission(ctx context.Context, params domain.NewPermissionParams) (*domain.Permission, error) {
	perm, err := domain.NewPermission(params)
	if err != nil {
		return nil, err
	}
	return uc.repo.Create(ctx, perm)
}

func (uc *PermissionUsecase) GetPermission(ctx context.Context, id int64) (*domain.Permission, error) {
	return uc.repo.GetByID(ctx, id)
}

func (uc *PermissionUsecase) ListPermissions(ctx context.Context, q domain.ListPermissionsQuery) ([]*domain.Permission, error) {
	return uc.repo.List(ctx, q)
}

// UpdatePermission 加载节点、校验并应用字段变更，再由仓储在写入事务内检查整树版本与结构约束。
func (uc *PermissionUsecase) UpdatePermission(ctx context.Context, id int64, params domain.NewPermissionParams, expectedRevision *int64) (*domain.Permission, error) {
	current, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := current.Update(params); err != nil {
		return nil, err
	}
	return uc.repo.Update(ctx, current, expectedRevision)
}

func (uc *PermissionUsecase) DeletePermission(ctx context.Context, id int64, expectedRevision *int64) error {
	return uc.repo.Delete(ctx, id, expectedRevision)
}

// GetMenusForRoles 汇总本实例策略中的角色权限，再筛选菜单并补全祖先，返回平铺列表与权限码。
// 两次读取不构成原子快照；结果用于前端展示，后续请求仍由中间件独立授权。
func (uc *PermissionUsecase) GetMenusForRoles(ctx context.Context, roleNames []string) ([]*domain.Permission, []domain.PermissionCode, error) {
	roles, err := toRoles(roleNames)
	if err != nil {
		return nil, nil, err
	}

	codes, err := uc.policy.ResolveCodes(ctx, roles)
	if err != nil {
		return nil, nil, err
	}

	perms, err := uc.repo.List(ctx, domain.ListPermissionsQuery{})
	if err != nil {
		return nil, nil, err
	}
	return domain.NewPermissionTree(perms).VisibleMenus(codes), codes, nil
}

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
