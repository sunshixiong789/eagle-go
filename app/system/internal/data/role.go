package data

import (
	"context"
	"fmt"

	"github.com/eagle-go/eagle/app/system/internal/biz"
	"github.com/eagle-go/eagle/pkg/db/sqlc"
)

type roleRepo struct {
	data *Data
}

// NewRoleRepo 构造角色仓储。
func NewRoleRepo(data *Data) biz.RoleRepo {
	return &roleRepo{data: data}
}

func toBizRole(r sqlc.SysRole) *biz.Role {
	return &biz.Role{
		ID:        r.ID,
		Name:      r.Name,
		Code:      r.Code,
		Sort:      r.Sort,
		DataScope: toInt32(r.DataScope),
		Status:    toInt32(r.Status),
		Remark:    r.Remark,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}

func (r *roleRepo) Create(ctx context.Context, role *biz.Role, permissionIDs []int64) (*biz.Role, error) {
	var created sqlc.SysRole

	err := r.data.db.Tx(ctx, func(q *sqlc.Queries) error {
		var err error
		created, err = q.CreateRole(ctx, sqlc.CreateRoleParams{
			Name:      role.Name,
			Code:      role.Code,
			Sort:      role.Sort,
			DataScope: toInt16(role.DataScope),
			Status:    toInt16(role.Status),
			Remark:    role.Remark,
		})
		if err != nil {
			return err
		}
		for _, pid := range permissionIDs {
			if err := q.AddRolePermission(ctx, sqlc.AddRolePermissionParams{
				RoleID:       created.ID,
				PermissionID: pid,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, biz.ErrRoleCodeDuplicated
		}
		if isForeignKeyViolation(err) {
			return nil, biz.ErrPermissionNotFound
		}
		return nil, fmt.Errorf("create role: %w", err)
	}

	out := toBizRole(created)
	out.PermissionIDs = permissionIDs
	return out, nil
}

func (r *roleRepo) GetByID(ctx context.Context, id int64) (*biz.Role, error) {
	role, err := r.data.db.GetRoleByID(ctx, id)
	if err != nil {
		if isNoRows(err) {
			return nil, biz.ErrRoleNotFound
		}
		return nil, fmt.Errorf("get role %d: %w", id, err)
	}
	return toBizRole(role), nil
}

func (r *roleRepo) GetByCode(ctx context.Context, code string) (*biz.Role, error) {
	role, err := r.data.db.GetRoleByCode(ctx, code)
	if err != nil {
		if isNoRows(err) {
			return nil, biz.ErrRoleNotFound
		}
		return nil, fmt.Errorf("get role by code %q: %w", code, err)
	}
	return toBizRole(role), nil
}

func (r *roleRepo) List(ctx context.Context, q biz.ListRolesQuery) ([]*biz.Role, int64, error) {
	rows, err := r.data.db.ListRoles(ctx, sqlc.ListRolesParams{
		Keyword:    nilIfEmpty(q.Keyword),
		Status:     int32PtrToInt16Ptr(q.Status),
		PageOffset: q.Offset,
		PageSize:   q.PageSize,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list roles: %w", err)
	}

	total, err := r.data.db.CountRoles(ctx, sqlc.CountRolesParams{
		Keyword: nilIfEmpty(q.Keyword),
		Status:  int32PtrToInt16Ptr(q.Status),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("count roles: %w", err)
	}

	roles := make([]*biz.Role, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, toBizRole(row))
	}
	return roles, total, nil
}

func (r *roleRepo) Update(ctx context.Context, role *biz.Role) (*biz.Role, error) {
	updated, err := r.data.db.UpdateRole(ctx, sqlc.UpdateRoleParams{
		ID:        role.ID,
		Name:      role.Name,
		Sort:      role.Sort,
		DataScope: toInt16(role.DataScope),
		Status:    toInt16(role.Status),
		Remark:    role.Remark,
	})
	if err != nil {
		if isNoRows(err) {
			return nil, biz.ErrRoleNotFound
		}
		return nil, fmt.Errorf("update role %d: %w", role.ID, err)
	}
	return toBizRole(updated), nil
}

func (r *roleRepo) Delete(ctx context.Context, id int64) error {
	rows, err := r.data.db.SoftDeleteRole(ctx, id)
	if err != nil {
		return fmt.Errorf("delete role %d: %w", id, err)
	}
	if rows == 0 {
		return biz.ErrRoleNotFound
	}
	return nil
}

// AssignPermissions 全量覆盖角色的权限，先删后插在同一事务内完成。
func (r *roleRepo) AssignPermissions(ctx context.Context, roleID int64, permissionIDs []int64) error {
	err := r.data.db.Tx(ctx, func(q *sqlc.Queries) error {
		if err := q.DeleteRolePermissions(ctx, roleID); err != nil {
			return err
		}
		for _, pid := range permissionIDs {
			if err := q.AddRolePermission(ctx, sqlc.AddRolePermissionParams{
				RoleID:       roleID,
				PermissionID: pid,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if isForeignKeyViolation(err) {
			return biz.ErrPermissionNotFound
		}
		return fmt.Errorf("assign permissions to role %d: %w", roleID, err)
	}
	return nil
}

func (r *roleRepo) ListPermissionIDs(ctx context.Context, roleID int64) ([]int64, error) {
	ids, err := r.data.db.ListPermissionIDsByRoleID(ctx, roleID)
	if err != nil {
		return nil, fmt.Errorf("list permission ids of role %d: %w", roleID, err)
	}
	return ids, nil
}

func (r *roleRepo) CountUsers(ctx context.Context, roleID int64) (int64, error) {
	count, err := r.data.db.CountUsersByRoleID(ctx, roleID)
	if err != nil {
		return 0, fmt.Errorf("count users of role %d: %w", roleID, err)
	}
	return count, nil
}

func (r *roleRepo) ListUserIDs(ctx context.Context, roleID int64) ([]int64, error) {
	ids, err := r.data.db.ListUserIDsByRoleID(ctx, roleID)
	if err != nil {
		return nil, fmt.Errorf("list user ids of role %d: %w", roleID, err)
	}
	return ids, nil
}

func (r *roleRepo) ListByUserID(ctx context.Context, userID int64) ([]*biz.Role, error) {
	rows, err := r.data.db.ListRolesByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list roles of user %d: %w", userID, err)
	}
	roles := make([]*biz.Role, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, toBizRole(row))
	}
	return roles, nil
}

func (r *roleRepo) InvalidatePermissionCache(ctx context.Context, userIDs ...int64) error {
	keys := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		keys = append(keys, userPermKey(id))
	}
	return r.data.invalidate(ctx, keys...)
}
