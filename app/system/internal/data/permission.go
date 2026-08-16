package data

import (
	"context"
	"fmt"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/eagle-go/eagle/app/system/internal/domain"
	"github.com/eagle-go/eagle/ent"
	"github.com/eagle-go/eagle/ent/permission"
	"github.com/eagle-go/eagle/ent/permissiondefinition"
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
func toDomainPermission(p *ent.Permission, revision int64) *domain.Permission {
	if p == nil {
		return nil
	}
	parentID := domain.RootPermissionID
	if p.ParentID != nil {
		parentID = *p.ParentID
	}
	code := ""
	if p.Code != nil {
		code = *p.Code
	}
	return domain.RehydratePermission(
		p.ID, parentID, p.Name, code, p.Type, p.Status,
		p.Path, p.Component, p.Icon, p.Sort, p.Visible,
		p.CreatedAt, p.UpdatedAt, revision,
	)
}

func (r *permissionRepo) Create(ctx context.Context, p *domain.Permission) (*domain.Permission, error) {
	tx, err := r.data.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin create permission: %w", err)
	}
	defer rollbackPermissionTxOnPanic(tx)
	state, err := lockPermissionTree(ctx, tx, nil)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if !p.IsRoot() {
		exists, err := tx.Permission.Query().Where(permission.IDEQ(p.ParentID())).Exist(ctx)
		if err != nil || !exists {
			_ = tx.Rollback()
			if err != nil {
				return nil, fmt.Errorf("check permission parent: %w", err)
			}
			return nil, domain.ErrPermissionNotFound
		}
	}
	if err := ensurePermissionDefinition(ctx, tx, p.Code()); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	created, err := tx.Permission.Create().
		SetNillableParentID(permissionParentPtr(p.ParentID())).
		SetName(p.Name()).
		SetNillableCode(permissionCodePtr(p.Code())).
		SetType(int32(p.Type())).
		SetPath(p.Path()).
		SetComponent(p.Component()).
		SetIcon(p.Icon()).
		SetSort(p.Sort()).
		SetVisible(p.Visible()).
		SetStatus(int32(p.Status())).
		Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		if isUniqueViolation(err) {
			return nil, domain.ErrPermissionCodeDuplicated
		}
		return nil, fmt.Errorf("create permission: %w", err)
	}
	next, err := bumpPermissionTreeRevision(ctx, tx, state.Revision)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create permission: %w", err)
	}
	return toDomainPermission(created, next), nil
}

func (r *permissionRepo) GetByID(ctx context.Context, id int64) (*domain.Permission, error) {
	revision, err := r.currentRevision(ctx)
	if err != nil {
		return nil, err
	}
	p, err := r.data.client.Permission.Get(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return nil, domain.ErrPermissionNotFound
		}
		return nil, fmt.Errorf("get permission %d: %w", id, err)
	}
	return toDomainPermission(p, revision), nil
}

// List 平铺返回权限。权限总量只有百级，一次全量取出比递归 CTE 更简单也更快。
func (r *permissionRepo) List(ctx context.Context, q domain.ListPermissionsQuery) ([]*domain.Permission, error) {
	revision, err := r.currentRevision(ctx)
	if err != nil {
		return nil, err
	}
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
		out = append(out, toDomainPermission(row, revision))
	}
	return out, nil
}

func (r *permissionRepo) Update(ctx context.Context, p *domain.Permission, expectedRevision *int64) (*domain.Permission, error) {
	tx, err := r.data.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin update permission: %w", err)
	}
	defer rollbackPermissionTxOnPanic(tx)
	state, err := lockPermissionTree(ctx, tx, expectedRevision)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	rows, err := tx.Permission.Query().All(ctx)
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("load permission tree for update: %w", err)
	}
	perms := make([]*domain.Permission, 0, len(rows))
	for _, row := range rows {
		perms = append(perms, toDomainPermission(row, state.Revision))
	}
	if err := domain.NewPermissionTree(perms).EnsureNoCycle(p.ID(), p.ParentID()); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := ensurePermissionDefinition(ctx, tx, p.Code()); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	updated, err := tx.Permission.UpdateOneID(p.ID()).
		SetNillableParentID(permissionParentPtr(p.ParentID())).
		SetName(p.Name()).
		SetNillableCode(permissionCodePtr(p.Code())).
		SetType(int32(p.Type())).
		SetPath(p.Path()).
		SetComponent(p.Component()).
		SetIcon(p.Icon()).
		SetSort(p.Sort()).
		SetVisible(p.Visible()).
		SetStatus(int32(p.Status())).
		Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		if isNotFound(err) {
			return nil, domain.ErrPermissionNotFound
		}
		if isUniqueViolation(err) {
			return nil, domain.ErrPermissionCodeDuplicated
		}
		return nil, fmt.Errorf("update permission %d: %w", p.ID(), err)
	}
	next, err := bumpPermissionTreeRevision(ctx, tx, state.Revision)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit update permission: %w", err)
	}
	return toDomainPermission(updated, next), nil
}

func (r *permissionRepo) Delete(ctx context.Context, id int64, expectedRevision *int64) error {
	tx, err := r.data.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin delete permission: %w", err)
	}
	defer rollbackPermissionTxOnPanic(tx)
	state, err := lockPermissionTree(ctx, tx, expectedRevision)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	children, err := tx.Permission.Query().Where(permission.ParentIDEQ(id)).Count(ctx)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("count children of permission %d: %w", id, err)
	}
	if children > 0 {
		_ = tx.Rollback()
		return domain.ErrPermissionHasChildren
	}
	if err := tx.Permission.DeleteOneID(id).Exec(ctx); err != nil {
		_ = tx.Rollback()
		if isNotFound(err) {
			return domain.ErrPermissionNotFound
		}
		return fmt.Errorf("delete permission %d: %w", id, err)
	}
	if _, err := bumpPermissionTreeRevision(ctx, tx, state.Revision); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete permission: %w", err)
	}
	return nil
}

func permissionParentPtr(parentID int64) *int64 {
	if parentID == domain.RootPermissionID {
		return nil
	}
	return &parentID
}

func permissionCodePtr(code domain.PermissionCode) *string {
	if code.IsZero() {
		return nil
	}
	value := code.String()
	return &value
}

func ensurePermissionDefinition(ctx context.Context, tx *ent.Tx, code domain.PermissionCode) error {
	if code.IsZero() {
		return nil
	}
	if err := tx.PermissionDefinition.Create().
		SetCode(code.String()).
		SetService(code.Domain()).
		SetResource(code.Resource()).
		SetAction(code.Action()).
		SetStatus(1).
		SetSource("navigation").
		OnConflict(entsql.ConflictColumns(permissiondefinition.FieldCode)).
		Ignore().
		Exec(ctx); err != nil {
		return fmt.Errorf("ensure permission definition %q: %w", code, err)
	}
	return nil
}

func (r *permissionRepo) currentRevision(ctx context.Context) (int64, error) {
	state, err := r.data.client.PermissionTreeState.Get(ctx, 1)
	if err != nil {
		return 0, fmt.Errorf("get permission tree revision: %w", err)
	}
	return state.Revision, nil
}

func (r *permissionRepo) KnownCodes(ctx context.Context) (map[string]struct{}, error) {
	rows, err := r.data.client.PermissionDefinition.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list permission definitions: %w", err)
	}
	out := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.Status == 1 {
			out[row.Code] = struct{}{}
		}
	}
	return out, nil
}

func lockPermissionTree(ctx context.Context, tx *ent.Tx, expected *int64) (*ent.PermissionTreeState, error) {
	// UPDATE 即使只改 updated_at 也会取得该单例行的排他锁，使所有树写入串行。
	state, err := tx.PermissionTreeState.UpdateOneID(1).SetUpdatedAt(time.Now()).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("lock permission tree: %w", err)
	}
	if expected != nil && *expected != state.Revision {
		return nil, domain.ErrConcurrentModification
	}
	return state, nil
}

func bumpPermissionTreeRevision(ctx context.Context, tx *ent.Tx, current int64) (int64, error) {
	state, err := tx.PermissionTreeState.UpdateOneID(1).SetRevision(current + 1).SetUpdatedAt(time.Now()).Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("bump permission tree revision: %w", err)
	}
	return state.Revision, nil
}

func rollbackPermissionTxOnPanic(tx *ent.Tx) {
	if p := recover(); p != nil {
		_ = tx.Rollback()
		panic(p)
	}
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
