package data

import (
	"context"
	"fmt"

	"github.com/eagle-go/eagle/app/system/internal/biz"
	"github.com/eagle-go/eagle/ent"
	"github.com/eagle-go/eagle/ent/permission"
)

type permissionRepo struct {
	data *Data
}

// NewPermissionRepo 构造权限仓储。
func NewPermissionRepo(data *Data) biz.PermissionRepo {
	return &permissionRepo{data: data}
}

func toBizPermission(p *ent.Permission) *biz.Permission {
	if p == nil {
		return nil
	}
	return &biz.Permission{
		ID:        p.ID,
		ParentID:  p.ParentID,
		Name:      p.Name,
		Code:      p.Code,
		Type:      p.Type,
		Path:      p.Path,
		Component: p.Component,
		Icon:      p.Icon,
		Sort:      p.Sort,
		Visible:   p.Visible,
		Status:    p.Status,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}
}

func (r *permissionRepo) Create(ctx context.Context, p *biz.Permission) (*biz.Permission, error) {
	created, err := r.data.client.Permission.Create().
		SetParentID(p.ParentID).
		SetName(p.Name).
		SetCode(p.Code).
		SetType(p.Type).
		SetPath(p.Path).
		SetComponent(p.Component).
		SetIcon(p.Icon).
		SetSort(p.Sort).
		SetVisible(p.Visible).
		SetStatus(p.Status).
		Save(ctx)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, biz.ErrPermissionCodeDuplicated
		}
		return nil, fmt.Errorf("create permission: %w", err)
	}
	return toBizPermission(created), nil
}

func (r *permissionRepo) GetByID(ctx context.Context, id int64) (*biz.Permission, error) {
	p, err := r.data.client.Permission.Get(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return nil, biz.ErrPermissionNotFound
		}
		return nil, fmt.Errorf("get permission %d: %w", id, err)
	}
	return toBizPermission(p), nil
}

// List 平铺返回权限，树由调用方按 ParentID 拼装。
// 权限总量只有百级，一次全量取出比递归 CTE 更简单也更快。
func (r *permissionRepo) List(ctx context.Context, q biz.ListPermissionsQuery) ([]*biz.Permission, error) {
	query := r.data.client.Permission.Query()
	if q.Status != nil {
		query = query.Where(permission.StatusEQ(*q.Status))
	}
	if q.Type != nil {
		query = query.Where(permission.TypeEQ(*q.Type))
	}

	rows, err := query.
		Order(ent.Asc(permission.FieldParentID), ent.Asc(permission.FieldSort), ent.Asc(permission.FieldID)).
		All(ctx)
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
	updated, err := r.data.client.Permission.UpdateOneID(p.ID).
		SetParentID(p.ParentID).
		SetName(p.Name).
		SetCode(p.Code).
		SetType(p.Type).
		SetPath(p.Path).
		SetComponent(p.Component).
		SetIcon(p.Icon).
		SetSort(p.Sort).
		SetVisible(p.Visible).
		SetStatus(p.Status).
		Save(ctx)
	if err != nil {
		if isNotFound(err) {
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
	err := r.data.client.Permission.DeleteOneID(id).Exec(ctx)
	if err != nil {
		if isNotFound(err) {
			return biz.ErrPermissionNotFound
		}
		return fmt.Errorf("delete permission %d: %w", id, err)
	}
	return nil
}

func (r *permissionRepo) CountChildren(ctx context.Context, id int64) (int64, error) {
	n, err := r.data.client.Permission.Query().
		Where(permission.ParentIDEQ(id)).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count children of permission %d: %w", id, err)
	}
	return int64(n), nil
}
