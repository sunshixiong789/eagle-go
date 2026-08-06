package data

import (
	"context"
	"fmt"

	"github.com/eagle-go/eagle/app/system/internal/biz"
	"github.com/eagle-go/eagle/pkg/db/sqlc"
)

type permissionRepo struct {
	data *Data
}

// NewPermissionRepo 构造权限仓储。
func NewPermissionRepo(data *Data) biz.PermissionRepo {
	return &permissionRepo{data: data}
}

func toBizPermission(p sqlc.SysPermission) *biz.Permission {
	return &biz.Permission{
		ID:        p.ID,
		ParentID:  p.ParentID,
		Name:      p.Name,
		Code:      p.Code,
		Type:      toInt32(p.Type),
		Path:      p.Path,
		Component: p.Component,
		Icon:      p.Icon,
		Sort:      p.Sort,
		Visible:   p.Visible,
		Status:    toInt32(p.Status),
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}
}

func (r *permissionRepo) Create(ctx context.Context, p *biz.Permission) (*biz.Permission, error) {
	created, err := r.data.db.CreatePermission(ctx, sqlc.CreatePermissionParams{
		ParentID:  p.ParentID,
		Name:      p.Name,
		Code:      p.Code,
		Type:      toInt16(p.Type),
		Path:      p.Path,
		Component: p.Component,
		Icon:      p.Icon,
		Sort:      p.Sort,
		Visible:   p.Visible,
		Status:    toInt16(p.Status),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, biz.ErrPermissionCodeDuplicated
		}
		return nil, fmt.Errorf("create permission: %w", err)
	}
	return toBizPermission(created), nil
}

func (r *permissionRepo) GetByID(ctx context.Context, id int64) (*biz.Permission, error) {
	p, err := r.data.db.GetPermissionByID(ctx, id)
	if err != nil {
		if isNoRows(err) {
			return nil, biz.ErrPermissionNotFound
		}
		return nil, fmt.Errorf("get permission %d: %w", id, err)
	}
	return toBizPermission(p), nil
}

func (r *permissionRepo) List(ctx context.Context, q biz.ListPermissionsQuery) ([]*biz.Permission, error) {
	rows, err := r.data.db.ListPermissions(ctx, sqlc.ListPermissionsParams{
		Status: int32PtrToInt16Ptr(q.Status),
		Type:   int32PtrToInt16Ptr(q.Type),
	})
	if err != nil {
		return nil, fmt.Errorf("list permissions: %w", err)
	}
	out := make([]*biz.Permission, 0, len(rows))
	for _, row := range rows {
		out = append(out, toBizPermission(row))
	}
	return out, nil
}

func (r *permissionRepo) Update(ctx context.Context, p *biz.Permission) (*biz.Permission, error) {
	updated, err := r.data.db.UpdatePermission(ctx, sqlc.UpdatePermissionParams{
		ID:        p.ID,
		ParentID:  p.ParentID,
		Name:      p.Name,
		Code:      p.Code,
		Type:      toInt16(p.Type),
		Path:      p.Path,
		Component: p.Component,
		Icon:      p.Icon,
		Sort:      p.Sort,
		Visible:   p.Visible,
		Status:    toInt16(p.Status),
	})
	if err != nil {
		if isNoRows(err) {
			return nil, biz.ErrPermissionNotFound
		}
		if isUniqueViolation(err) {
			return nil, biz.ErrPermissionCodeDuplicated
		}
		return nil, fmt.Errorf("update permission %d: %w", p.ID, err)
	}
	return toBizPermission(updated), nil
}

func (r *permissionRepo) Delete(ctx context.Context, id int64) error {
	rows, err := r.data.db.DeletePermission(ctx, id)
	if err != nil {
		return fmt.Errorf("delete permission %d: %w", id, err)
	}
	if rows == 0 {
		return biz.ErrPermissionNotFound
	}
	return nil
}

func (r *permissionRepo) CountChildren(ctx context.Context, id int64) (int64, error) {
	count, err := r.data.db.CountChildPermissions(ctx, id)
	if err != nil {
		return 0, fmt.Errorf("count children of permission %d: %w", id, err)
	}
	return count, nil
}

func (r *permissionRepo) ListByUserID(ctx context.Context, userID int64) ([]*biz.Permission, error) {
	rows, err := r.data.db.ListPermissionsByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list permissions of user %d: %w", userID, err)
	}
	out := make([]*biz.Permission, 0, len(rows))
	for _, row := range rows {
		out = append(out, toBizPermission(row))
	}
	return out, nil
}

func (r *permissionRepo) ListCodesByUserID(ctx context.Context, userID int64) ([]string, error) {
	return cached(ctx, r.data, userPermKey(userID), r.data.cache.permTTL,
		func(ctx context.Context) ([]string, error) {
			codes, err := r.data.db.ListPermissionCodesByUserID(ctx, userID)
			if err != nil {
				return nil, fmt.Errorf("list permission codes of user %d: %w", userID, err)
			}
			return codes, nil
		})
}

// InvalidateAllPermissionCache 清空所有用户的权限缓存。
//
// 权限节点本身（权限码、启用状态）变更会影响到所有持有该权限的用户，
// 逐个算出受影响用户的成本高于直接全清——权限变更是低频操作，
// 缓存重建的代价可以接受。
func (r *permissionRepo) InvalidateAllPermissionCache(ctx context.Context) error {
	return r.data.invalidateByPrefix(ctx, keyPrefixUserPerm)
}
