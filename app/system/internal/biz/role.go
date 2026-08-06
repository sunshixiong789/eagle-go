package biz

import (
	"context"
	"time"
)

// 数据权限范围。本期落库不参与判定，接入 Casbin 做数据权限时消费。
const (
	DataScopeAll        int32 = 1 // 全部数据
	DataScopeDeptAndSub int32 = 2 // 本部门及以下
	DataScopeDept       int32 = 3 // 本部门
	DataScopeSelf       int32 = 4 // 仅本人
)

// Role 是角色领域模型。
type Role struct {
	ID            int64
	Name          string
	Code          string
	Sort          int32
	DataScope     int32
	Status        int32
	Remark        string
	PermissionIDs []int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ListRolesQuery 是角色列表查询条件。
type ListRolesQuery struct {
	Keyword  string
	Status   *int32
	Offset   int32
	PageSize int32
}

// RoleRepo 由 data 层实现。
type RoleRepo interface {
	Create(ctx context.Context, r *Role, permissionIDs []int64) (*Role, error)
	GetByID(ctx context.Context, id int64) (*Role, error)
	GetByCode(ctx context.Context, code string) (*Role, error)
	List(ctx context.Context, q ListRolesQuery) ([]*Role, int64, error)
	Update(ctx context.Context, r *Role) (*Role, error)
	Delete(ctx context.Context, id int64) error
	AssignPermissions(ctx context.Context, roleID int64, permissionIDs []int64) error
	ListPermissionIDs(ctx context.Context, roleID int64) ([]int64, error)
	CountUsers(ctx context.Context, roleID int64) (int64, error)
	// ListUserIDs 用于在角色权限变更后精准失效受影响用户的缓存
	ListUserIDs(ctx context.Context, roleID int64) ([]int64, error)
	ListByUserID(ctx context.Context, userID int64) ([]*Role, error)
	InvalidatePermissionCache(ctx context.Context, userIDs ...int64) error
}

// RoleUsecase 编排角色相关的业务规则。
type RoleUsecase struct {
	repo RoleRepo
}

// NewRoleUsecase 构造角色用例。
func NewRoleUsecase(repo RoleRepo) *RoleUsecase {
	return &RoleUsecase{repo: repo}
}

// CreateRole 创建角色并绑定权限。
func (uc *RoleUsecase) CreateRole(ctx context.Context, r *Role, permissionIDs []int64) (*Role, error) {
	if _, err := uc.repo.GetByCode(ctx, r.Code); err == nil {
		return nil, ErrRoleCodeDuplicated
	}
	return uc.repo.Create(ctx, r, permissionIDs)
}

// GetRole 按 ID 取角色，并填充其权限 ID 列表。
func (uc *RoleUsecase) GetRole(ctx context.Context, id int64) (*Role, error) {
	r, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	ids, err := uc.repo.ListPermissionIDs(ctx, id)
	if err != nil {
		return nil, err
	}
	r.PermissionIDs = ids
	return r, nil
}

// ListRoles 分页查询角色。
func (uc *RoleUsecase) ListRoles(ctx context.Context, q ListRolesQuery) ([]*Role, int64, error) {
	return uc.repo.List(ctx, q)
}

// UpdateRole 更新角色。角色码不可改：它已写入存量 token 与权限缓存，
// 改动会造成已签发凭证的语义漂移。
func (uc *RoleUsecase) UpdateRole(ctx context.Context, r *Role) (*Role, error) {
	current, err := uc.repo.GetByID(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	if current.Code == BuiltinAdminRoleCode && r.Status == StatusDisabled {
		return nil, ErrRoleProtected
	}

	updated, err := uc.repo.Update(ctx, r)
	if err != nil {
		return nil, err
	}
	// 角色被停用会影响其下所有用户的权限，必须清缓存
	if current.Status != updated.Status {
		if err := uc.invalidateRoleMembers(ctx, r.ID); err != nil {
			return nil, err
		}
	}
	return updated, nil
}

// DeleteRole 删除角色。内置管理员角色受保护；仍有用户绑定时拒绝删除，
// 避免用户在无感知的情况下丢失全部权限。
func (uc *RoleUsecase) DeleteRole(ctx context.Context, id int64) error {
	current, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if current.Code == BuiltinAdminRoleCode {
		return ErrRoleProtected
	}

	count, err := uc.repo.CountUsers(ctx, id)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrRoleInUse
	}
	return uc.repo.Delete(ctx, id)
}

// AssignPermissions 全量覆盖角色的权限，并失效该角色下所有用户的权限缓存。
func (uc *RoleUsecase) AssignPermissions(ctx context.Context, roleID int64, permissionIDs []int64) error {
	if _, err := uc.repo.GetByID(ctx, roleID); err != nil {
		return err
	}
	if err := uc.repo.AssignPermissions(ctx, roleID, permissionIDs); err != nil {
		return err
	}
	return uc.invalidateRoleMembers(ctx, roleID)
}

// invalidateRoleMembers 清理该角色下全部用户的权限缓存。
// 不清理会导致权限调整后最长要等一个 TTL 才生效——收权限时这是安全问题。
func (uc *RoleUsecase) invalidateRoleMembers(ctx context.Context, roleID int64) error {
	userIDs, err := uc.repo.ListUserIDs(ctx, roleID)
	if err != nil {
		return err
	}
	if len(userIDs) == 0 {
		return nil
	}
	return uc.repo.InvalidatePermissionCache(ctx, userIDs...)
}
