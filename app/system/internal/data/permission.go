package data

import (
	"context"
	"fmt"

	"github.com/eagle-go/eagle/app/system/internal/domain"
	"github.com/eagle-go/eagle/ent"
	"github.com/eagle-go/eagle/ent/permission"
)

type permissionRepo struct {
	data *Data
}

// NewPermissionRepo 构造权限仓储。
func NewPermissionRepo(data *Data) domain.PermissionRepo {
	return &permissionRepo{data: data}
}

// toDomainPermission 从持久化状态重建聚合根。
//
// 走 Rehydrate 而不是 NewPermission：库里可能有规则收紧之前写入的
// 历史数据，用构造函数校验会让整张表读不出来。校验只在写入路径执行。
func toDomainPermission(p *ent.Permission) *domain.Permission {
	if p == nil {
		return nil
	}
	return domain.RehydratePermission(
		p.ID, p.ParentID, p.Name, p.Code, p.Type, p.Status,
		p.Path, p.Component, p.Icon, p.Sort, p.Visible,
		p.CreatedAt, p.UpdatedAt,
	)
}

func (r *permissionRepo) Create(ctx context.Context, p *domain.Permission) (*domain.Permission, error) {
	created, err := r.data.client.Permission.Create().
		SetParentID(p.ParentID()).
		SetName(p.Name()).
		SetCode(p.Code().String()).
		SetType(int32(p.Type())).
		SetPath(p.Path()).
		SetComponent(p.Component()).
		SetIcon(p.Icon()).
		SetSort(p.Sort()).
		SetVisible(p.Visible()).
		SetStatus(int32(p.Status())).
		Save(ctx)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, domain.ErrPermissionCodeDuplicated
		}
		return nil, fmt.Errorf("create permission: %w", err)
	}
	return toDomainPermission(created), nil
}

func (r *permissionRepo) GetByID(ctx context.Context, id int64) (*domain.Permission, error) {
	p, err := r.data.client.Permission.Get(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return nil, domain.ErrPermissionNotFound
		}
		return nil, fmt.Errorf("get permission %d: %w", id, err)
	}
	return toDomainPermission(p), nil
}

// List 平铺返回权限。权限总量只有百级，一次全量取出比递归 CTE 更简单也更快。
func (r *permissionRepo) List(ctx context.Context, q domain.ListPermissionsQuery) ([]*domain.Permission, error) {
	query := r.data.client.Permission.Query()
	if q.Status != nil {
		query = query.Where(permission.StatusEQ(int32(*q.Status)))
	}
	if q.Type != nil {
		query = query.Where(permission.TypeEQ(int32(*q.Type)))
	}

	rows, err := query.
		Order(ent.Asc(permission.FieldParentID), ent.Asc(permission.FieldSort), ent.Asc(permission.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list permissions: %w", err)
	}

	out := make([]*domain.Permission, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomainPermission(row))
	}
	return out, nil
}

func (r *permissionRepo) Update(ctx context.Context, p *domain.Permission) (*domain.Permission, error) {
	updated, err := r.data.client.Permission.UpdateOneID(p.ID()).
		SetParentID(p.ParentID()).
		SetName(p.Name()).
		SetCode(p.Code().String()).
		SetType(int32(p.Type())).
		SetPath(p.Path()).
		SetComponent(p.Component()).
		SetIcon(p.Icon()).
		SetSort(p.Sort()).
		SetVisible(p.Visible()).
		SetStatus(int32(p.Status())).
		Save(ctx)
	if err != nil {
		if isNotFound(err) {
			return nil, domain.ErrPermissionNotFound
		}
		if isUniqueViolation(err) {
			return nil, domain.ErrPermissionCodeDuplicated
		}
		return nil, fmt.Errorf("update permission %d: %w", p.ID(), err)
	}
	return toDomainPermission(updated), nil
}

func (r *permissionRepo) Delete(ctx context.Context, id int64) error {
	if err := r.data.client.Permission.DeleteOneID(id).Exec(ctx); err != nil {
		if isNotFound(err) {
			return domain.ErrPermissionNotFound
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
