package data

import (
	"context"
	"fmt"

	"github.com/eagle-go/eagle/app/system/internal/biz"
	"github.com/eagle-go/eagle/ent"
	"github.com/eagle-go/eagle/ent/dictdata"
	"github.com/eagle-go/eagle/ent/dicttype"
)

type dictRepo struct {
	data *Data
}

// NewDictRepo 构造字典仓储。
func NewDictRepo(data *Data) biz.DictRepo {
	return &dictRepo{data: data}
}

func toBizDictType(t *ent.DictType) *biz.DictType {
	if t == nil {
		return nil
	}
	return &biz.DictType{
		ID:        t.ID,
		Name:      t.Name,
		Type:      t.Type,
		Status:    t.Status,
		Remark:    t.Remark,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
}

func toBizDictData(d *ent.DictData) *biz.DictData {
	if d == nil {
		return nil
	}
	return &biz.DictData{
		ID:        d.ID,
		DictType:  d.DictType,
		Label:     d.Label,
		Value:     d.Value,
		Sort:      d.Sort,
		CSSClass:  d.CSSClass,
		IsDefault: d.IsDefault,
		Status:    d.Status,
		Remark:    d.Remark,
		CreatedAt: d.CreatedAt,
		UpdatedAt: d.UpdatedAt,
	}
}

// ── 字典类型 ──────────────────────────────────────────────

func (r *dictRepo) CreateType(ctx context.Context, t *biz.DictType) (*biz.DictType, error) {
	created, err := r.data.client.DictType.Create().
		SetName(t.Name).
		SetType(t.Type).
		SetStatus(t.Status).
		SetRemark(t.Remark).
		Save(ctx)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, biz.ErrDictTypeDuplicated
		}
		return nil, fmt.Errorf("create dict type: %w", err)
	}
	return toBizDictType(created), nil
}

func (r *dictRepo) GetTypeByID(ctx context.Context, id int64) (*biz.DictType, error) {
	t, err := r.data.client.DictType.Get(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return nil, biz.ErrDictTypeNotFound
		}
		return nil, fmt.Errorf("get dict type %d: %w", id, err)
	}
	return toBizDictType(t), nil
}

func (r *dictRepo) GetTypeByCode(ctx context.Context, code string) (*biz.DictType, error) {
	t, err := r.data.client.DictType.Query().
		Where(dicttype.TypeEQ(code)).
		Only(ctx)
	if err != nil {
		if isNotFound(err) {
			return nil, biz.ErrDictTypeNotFound
		}
		return nil, fmt.Errorf("get dict type %q: %w", code, err)
	}
	return toBizDictType(t), nil
}

func (r *dictRepo) ListTypes(ctx context.Context, q biz.ListDictTypesQuery) ([]*biz.DictType, int64, error) {
	query := r.data.client.DictType.Query()
	if q.Keyword != "" {
		query = query.Where(dicttype.Or(
			dicttype.NameContainsFold(q.Keyword),
			dicttype.TypeContainsFold(q.Keyword),
		))
	}
	if q.Status != nil {
		query = query.Where(dicttype.StatusEQ(*q.Status))
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

	out := make([]*biz.DictType, 0, len(rows))
	for _, row := range rows {
		out = append(out, toBizDictType(row))
	}
	return out, int64(total), nil
}

func (r *dictRepo) UpdateType(ctx context.Context, t *biz.DictType) (*biz.DictType, error) {
	updated, err := r.data.client.DictType.UpdateOneID(t.ID).
		SetName(t.Name).
		SetStatus(t.Status).
		SetRemark(t.Remark).
		Save(ctx)
	if err != nil {
		if isNotFound(err) {
			return nil, biz.ErrDictTypeNotFound
		}
		return nil, fmt.Errorf("update dict type %d: %w", t.ID, err)
	}
	return toBizDictType(updated), nil
}

func (r *dictRepo) DeleteType(ctx context.Context, id int64) error {
	if err := r.data.client.DictType.DeleteOneID(id).Exec(ctx); err != nil {
		if isNotFound(err) {
			return biz.ErrDictTypeNotFound
		}
		return fmt.Errorf("delete dict type %d: %w", id, err)
	}
	return nil
}

// ── 字典项 ────────────────────────────────────────────────

func (r *dictRepo) CreateData(ctx context.Context, d *biz.DictData) (*biz.DictData, error) {
	created, err := r.data.client.DictData.Create().
		SetDictType(d.DictType).
		SetLabel(d.Label).
		SetValue(d.Value).
		SetSort(d.Sort).
		SetCSSClass(d.CSSClass).
		SetIsDefault(d.IsDefault).
		SetStatus(d.Status).
		SetRemark(d.Remark).
		Save(ctx)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, biz.ErrDictDataDuplicated
		}
		// dict_type 有外键指向 sys_dict_type.type，
		// 挂到不存在的类型下会触发外键冲突
		if isForeignKeyViolation(err) {
			return nil, biz.ErrDictTypeNotFound
		}
		return nil, fmt.Errorf("create dict data: %w", err)
	}
	return toBizDictData(created), nil
}

func (r *dictRepo) GetDataByID(ctx context.Context, id int64) (*biz.DictData, error) {
	d, err := r.data.client.DictData.Get(ctx, id)
	if err != nil {
		if isNotFound(err) {
			return nil, biz.ErrDictDataNotFound
		}
		return nil, fmt.Errorf("get dict data %d: %w", id, err)
	}
	return toBizDictData(d), nil
}

func (r *dictRepo) ListData(ctx context.Context, q biz.ListDictDataQuery) ([]*biz.DictData, int64, error) {
	query := r.data.client.DictData.Query()
	if q.DictType != nil {
		query = query.Where(dictdata.DictTypeEQ(*q.DictType))
	}
	if q.Keyword != "" {
		query = query.Where(dictdata.LabelContainsFold(q.Keyword))
	}
	if q.Status != nil {
		query = query.Where(dictdata.StatusEQ(*q.Status))
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

	out := make([]*biz.DictData, 0, len(rows))
	for _, row := range rows {
		out = append(out, toBizDictData(row))
	}
	return out, int64(total), nil
}

// ListDataByType 是前端下拉框的主要来源，命中率高，走缓存。
func (r *dictRepo) ListDataByType(ctx context.Context, dictType string) ([]*biz.DictData, error) {
	return cached(ctx, r.data, dictDataKey(dictType), r.data.cache.dictTTL,
		func(ctx context.Context) ([]*biz.DictData, error) {
			rows, err := r.data.client.DictData.Query().
				Where(
					dictdata.DictTypeEQ(dictType),
					dictdata.StatusEQ(biz.StatusEnabled),
				).
				Order(ent.Asc(dictdata.FieldSort), ent.Asc(dictdata.FieldID)).
				All(ctx)
			if err != nil {
				return nil, fmt.Errorf("list dict data of type %q: %w", dictType, err)
			}
			out := make([]*biz.DictData, 0, len(rows))
			for _, row := range rows {
				out = append(out, toBizDictData(row))
			}
			return out, nil
		})
}

func (r *dictRepo) UpdateData(ctx context.Context, d *biz.DictData) (*biz.DictData, error) {
	updated, err := r.data.client.DictData.UpdateOneID(d.ID).
		SetLabel(d.Label).
		SetValue(d.Value).
		SetSort(d.Sort).
		SetCSSClass(d.CSSClass).
		SetIsDefault(d.IsDefault).
		SetStatus(d.Status).
		SetRemark(d.Remark).
		Save(ctx)
	if err != nil {
		if isNotFound(err) {
			return nil, biz.ErrDictDataNotFound
		}
		if isUniqueViolation(err) {
			return nil, biz.ErrDictDataDuplicated
		}
		return nil, fmt.Errorf("update dict data %d: %w", d.ID, err)
	}
	return toBizDictData(updated), nil
}

func (r *dictRepo) DeleteData(ctx context.Context, id int64) error {
	if err := r.data.client.DictData.DeleteOneID(id).Exec(ctx); err != nil {
		if isNotFound(err) {
			return biz.ErrDictDataNotFound
		}
		return fmt.Errorf("delete dict data %d: %w", id, err)
	}
	return nil
}

func (r *dictRepo) InvalidateDictCache(ctx context.Context, dictTypes ...string) error {
	keys := make([]string, 0, len(dictTypes))
	for _, t := range dictTypes {
		keys = append(keys, dictDataKey(t))
	}
	return r.data.invalidate(ctx, keys...)
}
