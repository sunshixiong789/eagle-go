package data

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eagle-go/eagle/app/system/internal/domain"
	"github.com/eagle-go/eagle/ent"
	"github.com/eagle-go/eagle/ent/dictdata"
	"github.com/eagle-go/eagle/ent/dicttype"
)

type dictRepo struct {
	data *Data
}

// NewDictRepo 构造字典仓储。
func NewDictRepo(data *Data) domain.DictRepo {
	return &dictRepo{data: data}
}

func toDomainDictType(t *ent.DictType) *domain.DictType {
	if t == nil {
		return nil
	}
	return &domain.DictType{
		ID:        t.ID,
		Name:      t.Name,
		Type:      t.Type,
		Status:    domain.Status(t.Status),
		Remark:    t.Remark,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
}

func toDomainDictData(d *ent.DictData) *domain.DictData {
	if d == nil {
		return nil
	}
	return &domain.DictData{
		ID:        d.ID,
		DictType:  d.DictType,
		Label:     d.Label,
		Value:     d.Value,
		Sort:      d.Sort,
		CSSClass:  d.CSSClass,
		IsDefault: d.IsDefault,
		Status:    domain.Status(d.Status),
		Remark:    d.Remark,
		CreatedAt: d.CreatedAt,
		UpdatedAt: d.UpdatedAt,
	}
}

// ── 字典类型 ──────────────────────────────────────────────

func (r *dictRepo) CreateType(ctx context.Context, t *domain.DictType) (*domain.DictType, error) {
	created, err := r.data.client.DictType.Create().
		SetName(t.Name).
		SetType(t.Type).
		SetStatus(int32(t.Status)).
		SetRemark(t.Remark).
		Save(ctx)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, domain.ErrDictTypeDuplicated
		}
		return nil, fmt.Errorf("create dict type: %w", err)
	}
	return toDomainDictType(created), nil
}

func (r *dictRepo) GetTypeByID(ctx context.Context, id int64) (*domain.DictType, error) {
	t, err := r.data.client.DictType.Get(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return nil, domain.ErrDictTypeNotFound
		}
		return nil, fmt.Errorf("get dict type %d: %w", id, err)
	}
	return toDomainDictType(t), nil
}

func (r *dictRepo) GetTypeByCode(ctx context.Context, code string) (*domain.DictType, error) {
	t, err := r.data.client.DictType.Query().
		Where(dicttype.TypeEQ(code)).
		Only(ctx)
	if err != nil {
		if isNotFound(err) {
			return nil, domain.ErrDictTypeNotFound
		}
		return nil, fmt.Errorf("get dict type %q: %w", code, err)
	}
	return toDomainDictType(t), nil
}

func (r *dictRepo) ListTypes(ctx context.Context, q domain.ListDictTypesQuery) ([]*domain.DictType, int64, error) {
	query := r.data.client.DictType.Query()
	if q.Keyword != "" {
		query = query.Where(dicttype.Or(
			dicttype.NameContainsFold(q.Keyword),
			dicttype.TypeContainsFold(q.Keyword),
		))
	}
	if q.Status != nil {
		query = query.Where(dicttype.StatusEQ(int32(*q.Status)))
	}

	// 先数总数再取当页：两次查询共用同一组谓词，避免翻页时
	// 总数与列表来自不同的过滤条件
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count dict types: %w", err)
	}

	rows, err := query.
		Order(ent.Asc(dicttype.FieldID)).
		Offset(int(q.Offset)).
		Limit(int(q.PageSize)).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list dict types: %w", err)
	}

	out := make([]*domain.DictType, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomainDictType(row))
	}
	return out, int64(total), nil
}

func (r *dictRepo) UpdateType(ctx context.Context, t *domain.DictType) (*domain.DictType, error) {
	current, err := r.GetTypeByID(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	updated, err := r.data.client.DictType.UpdateOneID(t.ID).
		SetName(t.Name).
		SetStatus(int32(t.Status)).
		SetRemark(t.Remark).
		Save(ctx)
	if err != nil {
		if isNotFound(err) {
			return nil, domain.ErrDictTypeNotFound
		}
		return nil, fmt.Errorf("update dict type %d: %w", t.ID, err)
	}
	r.invalidateCache(ctx, current.Type)
	return toDomainDictType(updated), nil
}

func (r *dictRepo) DeleteType(ctx context.Context, id int64) error {
	current, err := r.GetTypeByID(ctx, id)
	if err != nil {
		return err
	}
	if err := r.data.client.DictType.DeleteOneID(id).Exec(ctx); err != nil {
		if isNotFound(err) {
			return domain.ErrDictTypeNotFound
		}
		return fmt.Errorf("delete dict type %d: %w", id, err)
	}
	r.invalidateCache(ctx, current.Type)
	return nil
}

// ── 字典项 ────────────────────────────────────────────────

func (r *dictRepo) CreateData(ctx context.Context, d *domain.DictData) (*domain.DictData, error) {
	created, err := r.data.client.DictData.Create().
		SetDictType(d.DictType).
		SetLabel(d.Label).
		SetValue(d.Value).
		SetSort(d.Sort).
		SetCSSClass(d.CSSClass).
		SetIsDefault(d.IsDefault).
		SetStatus(int32(d.Status)).
		SetRemark(d.Remark).
		Save(ctx)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, domain.ErrDictDataDuplicated
		}
		// dict_type 有外键指向 sys_dict_type.type，
		// 挂到不存在的类型下会触发外键冲突
		if isForeignKeyViolation(err) {
			return nil, domain.ErrDictTypeNotFound
		}
		return nil, fmt.Errorf("create dict data: %w", err)
	}
	r.invalidateCache(ctx, d.DictType)
	return toDomainDictData(created), nil
}

func (r *dictRepo) GetDataByID(ctx context.Context, id int64) (*domain.DictData, error) {
	d, err := r.data.client.DictData.Get(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return nil, domain.ErrDictDataNotFound
		}
		return nil, fmt.Errorf("get dict data %d: %w", id, err)
	}
	return toDomainDictData(d), nil
}

func (r *dictRepo) ListData(ctx context.Context, q domain.ListDictDataQuery) ([]*domain.DictData, int64, error) {
	query := r.data.client.DictData.Query()
	if q.DictType != nil {
		query = query.Where(dictdata.DictTypeEQ(*q.DictType))
	}
	if q.Keyword != "" {
		query = query.Where(dictdata.LabelContainsFold(q.Keyword))
	}
	if q.Status != nil {
		query = query.Where(dictdata.StatusEQ(int32(*q.Status)))
	}

	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count dict data: %w", err)
	}

	rows, err := query.
		Order(ent.Asc(dictdata.FieldDictType), ent.Asc(dictdata.FieldSort), ent.Asc(dictdata.FieldID)).
		Offset(int(q.Offset)).
		Limit(int(q.PageSize)).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list dict data: %w", err)
	}

	out := make([]*domain.DictData, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDomainDictData(row))
	}
	return out, int64(total), nil
}

// ListDataByType 是前端下拉框的主要来源，命中率高，走缓存。
func (r *dictRepo) ListDataByType(ctx context.Context, dictType string) ([]*domain.DictData, error) {
	return r.data.dictCache.Get(ctx, dictDataKey(dictType),
		func(ctx context.Context) ([]*domain.DictData, error) {
			rows, err := r.data.client.DictData.Query().
				Where(
					dictdata.DictTypeEQ(dictType),
					dictdata.StatusEQ(int32(domain.StatusEnabled)),
				).
				Order(ent.Asc(dictdata.FieldSort), ent.Asc(dictdata.FieldID)).
				All(ctx)
			if err != nil {
				return nil, fmt.Errorf("list dict data of type %q: %w", dictType, err)
			}
			out := make([]*domain.DictData, 0, len(rows))
			for _, row := range rows {
				out = append(out, toDomainDictData(row))
			}
			return out, nil
		})
}

func (r *dictRepo) UpdateData(ctx context.Context, d *domain.DictData) (*domain.DictData, error) {
	updated, err := r.data.client.DictData.UpdateOneID(d.ID).
		SetLabel(d.Label).
		SetValue(d.Value).
		SetSort(d.Sort).
		SetCSSClass(d.CSSClass).
		SetIsDefault(d.IsDefault).
		SetStatus(int32(d.Status)).
		SetRemark(d.Remark).
		Save(ctx)
	if err != nil {
		if isNotFound(err) {
			return nil, domain.ErrDictDataNotFound
		}
		if isUniqueViolation(err) {
			return nil, domain.ErrDictDataDuplicated
		}
		return nil, fmt.Errorf("update dict data %d: %w", d.ID, err)
	}
	r.invalidateCache(ctx, d.DictType)
	return toDomainDictData(updated), nil
}

func (r *dictRepo) DeleteData(ctx context.Context, id int64) error {
	current, err := r.GetDataByID(ctx, id)
	if err != nil {
		return err
	}
	if err := r.data.client.DictData.DeleteOneID(id).Exec(ctx); err != nil {
		if isNotFound(err) {
			return domain.ErrDictDataNotFound
		}
		return fmt.Errorf("delete dict data %d: %w", id, err)
	}
	r.invalidateCache(ctx, current.DictType)
	return nil
}

// InvalidateCache 保留为测试和运维工具入口；领域接口不再暴露缓存概念。
func (r *dictRepo) InvalidateCache(ctx context.Context, dictTypes ...string) error {
	keys := make([]string, 0, len(dictTypes))
	for _, t := range dictTypes {
		keys = append(keys, dictDataKey(t))
	}
	return r.data.dictCache.Invalidate(ctx, keys...)
}

func (r *dictRepo) invalidateCache(ctx context.Context, dictTypes ...string) {
	if err := r.InvalidateCache(ctx, dictTypes...); err != nil {
		// 数据库写入已经提交，不能把缓存删除失败伪装成整次业务失败。
		// Redis 恢复前读取会自动回源；恢复后最迟在 TTL 到期时收敛。
		slog.Default().WarnContext(ctx, "字典数据已提交，但缓存失效失败", "dict_types", dictTypes, "error", err)
	}
}
