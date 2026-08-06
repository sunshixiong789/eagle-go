package biz

import (
	"context"
	"time"
)

// 权限节点类型。
const (
	PermissionTypeDir    int32 = 1 // 目录
	PermissionTypeMenu   int32 = 2 // 菜单
	PermissionTypeButton int32 = 3 // 按钮
)

// RootPermissionID 是顶级节点的 parent_id 取值。
const RootPermissionID int64 = 0

// 状态取值。
const (
	StatusDisabled int32 = 0
	StatusEnabled  int32 = 1
)

// Permission 是权限领域模型，同时承载菜单树结构。
type Permission struct {
	ID        int64
	ParentID  int64
	Name      string
	Code      string
	Type      int32
	Path      string
	Component string
	Icon      string
	Sort      int32
	Visible   bool
	Status    int32
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ListPermissionsQuery 是权限列表查询条件。
type ListPermissionsQuery struct {
	Status *int32
	Type   *int32
}

// PermissionRepo 由 data 层实现。
type PermissionRepo interface {
	Create(ctx context.Context, p *Permission) (*Permission, error)
	GetByID(ctx context.Context, id int64) (*Permission, error)
	List(ctx context.Context, q ListPermissionsQuery) ([]*Permission, error)
	Update(ctx context.Context, p *Permission) (*Permission, error)
	Delete(ctx context.Context, id int64) error
	CountChildren(ctx context.Context, id int64) (int64, error)
}

// PermissionUsecase 编排权限相关的业务规则。
type PermissionUsecase struct {
	repo   PermissionRepo
	policy PolicyStore
}

// NewPermissionUsecase 构造权限用例。
func NewPermissionUsecase(repo PermissionRepo, policy PolicyStore) *PermissionUsecase {
	return &PermissionUsecase{repo: repo, policy: policy}
}

// CreatePermission 新建权限节点。
func (uc *PermissionUsecase) CreatePermission(ctx context.Context, p *Permission) (*Permission, error) {
	if p.ParentID != RootPermissionID {
		if _, err := uc.repo.GetByID(ctx, p.ParentID); err != nil {
			return nil, ErrPermissionNotFound
		}
	}
	return uc.repo.Create(ctx, p)
}

// GetPermission 按 ID 取权限。
func (uc *PermissionUsecase) GetPermission(ctx context.Context, id int64) (*Permission, error) {
	return uc.repo.GetByID(ctx, id)
}

// ListPermissions 返回平铺的权限列表，树由调用方按 ParentID 拼装。
func (uc *PermissionUsecase) ListPermissions(ctx context.Context, q ListPermissionsQuery) ([]*Permission, error) {
	return uc.repo.List(ctx, q)
}

// UpdatePermission 更新权限节点，并拦截会形成环的 parent 变更。
func (uc *PermissionUsecase) UpdatePermission(ctx context.Context, p *Permission) (*Permission, error) {
	current, err := uc.repo.GetByID(ctx, p.ID)
	if err != nil {
		return nil, err
	}

	if p.ParentID != current.ParentID {
		if err := uc.ensureNoCycle(ctx, p.ID, p.ParentID); err != nil {
			return nil, err
		}
	}
	return uc.repo.Update(ctx, p)
}

// DeletePermission 删除权限节点。存在子节点时拒绝，避免留下孤儿节点。
func (uc *PermissionUsecase) DeletePermission(ctx context.Context, id int64) error {
	if _, err := uc.repo.GetByID(ctx, id); err != nil {
		return err
	}
	count, err := uc.repo.CountChildren(ctx, id)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrPermissionHasChildren
	}
	return uc.repo.Delete(ctx, id)
}

// GetMenusForRoles 返回给定角色可见的菜单树与权限码。
//
// 菜单不再按「用户 ID」查库：用户的角色由 Keycloak 随 token 下发，
// 本服务据角色向 Casbin 要权限码，再用权限码过滤权限树。
// 这样菜单与鉴权判定共用同一份策略，不会出现「菜单看得见但点了 403」。
func (uc *PermissionUsecase) GetMenusForRoles(ctx context.Context, roles []string) ([]*Permission, []string, error) {
	codes, err := uc.policy.PermissionsOf(ctx, roles)
	if err != nil {
		return nil, nil, err
	}

	all, err := uc.repo.List(ctx, ListPermissionsQuery{Status: ptr(StatusEnabled)})
	if err != nil {
		return nil, nil, err
	}

	granted := make(map[string]struct{}, len(codes))
	for _, c := range codes {
		granted[c] = struct{}{}
	}

	// 先挑出有权限的叶子与菜单
	visible := make(map[int64]*Permission, len(all))
	byID := make(map[int64]*Permission, len(all))
	for _, p := range all {
		byID[p.ID] = p
	}
	for _, p := range all {
		if p.Code == "" {
			// 目录本身没有权限码，靠子节点带出来
			continue
		}
		if _, ok := granted[p.Code]; ok {
			visible[p.ID] = p
		}
	}

	// 再把可见节点的祖先链补上，否则菜单会因为父目录缺失而挂不上树
	for id := range visible {
		for cur := byID[id]; cur != nil && cur.ParentID != RootPermissionID; {
			parent, ok := byID[cur.ParentID]
			if !ok {
				break
			}
			if _, seen := visible[parent.ID]; seen {
				break
			}
			visible[parent.ID] = parent
			cur = parent
		}
	}

	menus := make([]*Permission, 0, len(visible))
	for _, p := range all {
		if _, ok := visible[p.ID]; !ok {
			continue
		}
		// 按钮不进菜单树，前端用权限码单独控制显隐
		if p.Type != PermissionTypeButton {
			menus = append(menus, p)
		}
	}
	return menus, codes, nil
}

// ensureNoCycle 校验把 id 挂到 newParentID 之下不会形成环。
// 逐级上溯 newParentID 的祖先链，遇到 id 即说明 newParentID 是 id 的后代。
func (uc *PermissionUsecase) ensureNoCycle(ctx context.Context, id, newParentID int64) error {
	if newParentID == RootPermissionID {
		return nil
	}
	if newParentID == id {
		return ErrPermissionCycle
	}

	// 树深有限，加上限防止数据本身已成环时无限循环
	const maxDepth = 32
	cursor := newParentID
	for i := 0; i < maxDepth; i++ {
		node, err := uc.repo.GetByID(ctx, cursor)
		if err != nil {
			return ErrPermissionNotFound
		}
		if node.ParentID == RootPermissionID {
			return nil
		}
		if node.ParentID == id {
			return ErrPermissionCycle
		}
		cursor = node.ParentID
	}
	return ErrPermissionCycle
}

func ptr[T any](v T) *T { return &v }
