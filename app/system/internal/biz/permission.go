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
	ListByUserID(ctx context.Context, userID int64) ([]*Permission, error)
	ListCodesByUserID(ctx context.Context, userID int64) ([]string, error)
	// InvalidateAllPermissionCache 在权限本身变更（而非授权关系变更）后
	// 清空全部用户的权限缓存
	InvalidateAllPermissionCache(ctx context.Context) error
}

// PermissionUsecase 编排权限相关的业务规则。
type PermissionUsecase struct {
	repo PermissionRepo
}

// NewPermissionUsecase 构造权限用例。
func NewPermissionUsecase(repo PermissionRepo) *PermissionUsecase {
	return &PermissionUsecase{repo: repo}
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

	updated, err := uc.repo.Update(ctx, p)
	if err != nil {
		return nil, err
	}
	// 权限码或状态变了，所有人的权限集合都可能受影响
	if current.Code != updated.Code || current.Status != updated.Status {
		if err := uc.repo.InvalidateAllPermissionCache(ctx); err != nil {
			return nil, err
		}
	}
	return updated, nil
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
	if err := uc.repo.Delete(ctx, id); err != nil {
		return err
	}
	return uc.repo.InvalidateAllPermissionCache(ctx)
}

// GetUserMenus 返回用户可见的目录与菜单，以及全部权限码。
// 前端用前者渲染路由、用后者做按钮级显隐。
func (uc *PermissionUsecase) GetUserMenus(ctx context.Context, userID int64) ([]*Permission, []string, error) {
	all, err := uc.repo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, nil, err
	}

	menus := make([]*Permission, 0, len(all))
	codes := make([]string, 0, len(all))
	for _, p := range all {
		if p.Type != PermissionTypeButton {
			menus = append(menus, p)
		}
		if p.Code != "" {
			codes = append(codes, p.Code)
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

	// 树深有限，加个上限防止数据本身已经成环时无限循环
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
	// 走到深度上限说明数据里已经存在环
	return ErrPermissionCycle
}
