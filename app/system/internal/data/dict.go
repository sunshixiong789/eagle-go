package data

import (
	"context"
	"fmt"

	"github.com/eagle-go/eagle/app/system/internal/biz"
	"github.com/eagle-go/eagle/pkg/db/sqlc"
)

type dictRepo struct {
	data *Data
}

// NewDictRepo 构造字典仓储。
func NewDictRepo(data *Data) biz.DictRepo {
	return &dictRepo{data: data}
}

func toBizDictType(t sqlc.SysDictType) *biz.DictType {
	return &biz.DictType{
		ID:        t.ID,
		Name:      t.Name,
		Type:      t.Type,
		Status:    toInt32(t.Status),
		Remark:    t.Remark,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
}

// 注：sqlc 由表名 sys_dict_data 生成的结构体名是 SysDictDatum（单数化的产物）。
func toBizDictData(d sqlc.SysDictDatum) *biz.DictData {
	return &biz.DictData{
		ID:        d.ID,
		DictType:  d.DictType,
		Label:     d.Label,
		Value:     d.Value,
		Sort:      d.Sort,
		CSSClass:  d.CssClass,
		IsDefault: d.IsDefault,
		Status:    toInt32(d.Status),
		Remark:    d.Remark,
		CreatedAt: d.CreatedAt,
		UpdatedAt: d.UpdatedAt,
	}
}

// ── 字典类型 ──────────────────────────────────────────────

func (r *dictRepo) CreateType(ctx context.Context, t *biz.DictType) (*biz.DictType, error) {
	created, err := r.data.db.CreateDictType(ctx, sqlc.CreateDictTypeParams{
		Name:   t.Name,
		Type:   t.Type,
		Status: toInt16(t.Status),
		Remark: t.Remark,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, biz.ErrDictTypeDuplicated
		}
		return nil, fmt.Errorf("create dict type: %w", err)
	}
	return toBizDictType(created), nil
}

func (r *dictRepo) GetTypeByID(ctx context.Context, id int64) (*biz.DictType, error) {
	t, err := r.data.db.GetDictTypeByID(ctx, id)
	if err != nil {
		if isNoRows(err) {
			return nil, biz.ErrDictTypeNotFound
		}
		return nil, fmt.Errorf("get dict type %d: %w", id, err)
	}
	return toBizDictType(t), nil
}

func (r *dictRepo) GetTypeByCode(ctx context.Context, code string) (*biz.DictType, error) {
	t, err := r.data.db.GetDictTypeByType(ctx, code)
	if err != nil {
		if isNoRows(err) {
			return nil, biz.ErrDictTypeNotFound
		}
		return nil, fmt.Errorf("get dict type %q: %w", code, err)
	}
	return toBizDictType(t), nil
}

func (r *dictRepo) ListTypes(ctx context.Context, q biz.ListDictTypesQuery) ([]*biz.DictType, int64, error) {
	rows, err := r.data.db.ListDictTypes(ctx, sqlc.ListDictTypesParams{
		Keyword:    nilIfEmpty(q.Keyword),
		Status:     int32PtrToInt16Ptr(q.Status),
		PageOffset: q.Offset,
		PageSize:   q.PageSize,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list dict types: %w", err)
	}

	total, err := r.data.db.CountDictTypes(ctx, sqlc.CountDictTypesParams{
		Keyword: nilIfEmpty(q.Keyword),
		Status:  int32PtrToInt16Ptr(q.Status),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("count dict types: %w", err)
	}

	out := make([]*biz.DictType, 0, len(rows))
	for _, row := range rows {
		out = append(out, toBizDictType(row))
	}
	return out, total, nil
}

func (r *dictRepo) UpdateType(ctx context.Context, t *biz.DictType) (*biz.DictType, error) {
	updated, err := r.data.db.UpdateDictType(ctx, sqlc.UpdateDictTypeParams{
		ID:     t.ID,
		Name:   t.Name,
		Status: toInt16(t.Status),
		Remark: t.Remark,
	})
	if err != nil {
		if isNoRows(err) {
			return nil, biz.ErrDictTypeNotFound
		}
		return nil, fmt.Errorf("update dict type %d: %w", t.ID, err)
	}
	return toBizDictType(updated), nil
}

func (r *dictRepo) DeleteType(ctx context.Context, id int64) error {
	rows, err := r.data.db.DeleteDictType(ctx, id)
	if err != nil {
		return fmt.Errorf("delete dict type %d: %w", id, err)
	}
	if rows == 0 {
		return biz.ErrDictTypeNotFound
	}
	return nil
}

// ── 字典项 ────────────────────────────────────────────────

func (r *dictRepo) CreateData(ctx context.Context, d *biz.DictData) (*biz.DictData, error) {
	created, err := r.data.db.CreateDictData(ctx, sqlc.CreateDictDataParams{
		DictType:  d.DictType,
		Label:     d.Label,
		Value:     d.Value,
		Sort:      d.Sort,
		CssClass:  d.CSSClass,
		IsDefault: d.IsDefault,
		Status:    toInt16(d.Status),
		Remark:    d.Remark,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, biz.ErrDictDataDuplicated
		}
		if isForeignKeyViolation(err) {
			return nil, biz.ErrDictTypeNotFound
		}
		return nil, fmt.Errorf("create dict data: %w", err)
	}
	return toBizDictData(created), nil
}

func (r *dictRepo) GetDataByID(ctx context.Context, id int64) (*biz.DictData, error) {
	d, err := r.data.db.GetDictDataByID(ctx, id)
	if err != nil {
		if isNoRows(err) {
			return nil, biz.ErrDictDataNotFound
		}
		return nil, fmt.Errorf("get dict data %d: %w", id, err)
	}
	return toBizDictData(d), nil
}

func (r *dictRepo) ListData(ctx context.Context, q biz.ListDictDataQuery) ([]*biz.DictData, int64, error) {
	rows, err := r.data.db.ListDictData(ctx, sqlc.ListDictDataParams{
		DictType:   q.DictType,
		Keyword:    nilIfEmpty(q.Keyword),
		Status:     int32PtrToInt16Ptr(q.Status),
		PageOffset: q.Offset,
		PageSize:   q.PageSize,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list dict data: %w", err)
	}

	total, err := r.data.db.CountDictData(ctx, sqlc.CountDictDataParams{
		DictType: q.DictType,
		Keyword:  nilIfEmpty(q.Keyword),
		Status:   int32PtrToInt16Ptr(q.Status),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("count dict data: %w", err)
	}

	out := make([]*biz.DictData, 0, len(rows))
	for _, row := range rows {
		out = append(out, toBizDictData(row))
	}
	return out, total, nil
}

// ListDataByType 是前端下拉框的主要来源，命中率高，走缓存。
func (r *dictRepo) ListDataByType(ctx context.Context, dictType string) ([]*biz.DictData, error) {
	return cached(ctx, r.data, dictDataKey(dictType), r.data.cache.dictTTL,
		func(ctx context.Context) ([]*biz.DictData, error) {
			rows, err := r.data.db.ListDictDataByType(ctx, dictType)
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
	updated, err := r.data.db.UpdateDictData(ctx, sqlc.UpdateDictDataParams{
		ID:        d.ID,
		Label:     d.Label,
		Value:     d.Value,
		Sort:      d.Sort,
		CssClass:  d.CSSClass,
		IsDefault: d.IsDefault,
		Status:    toInt16(d.Status),
		Remark:    d.Remark,
	})
	if err != nil {
		if isNoRows(err) {
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
	rows, err := r.data.db.DeleteDictData(ctx, id)
	if err != nil {
		return fmt.Errorf("delete dict data %d: %w", id, err)
	}
	if rows == 0 {
		return biz.ErrDictDataNotFound
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
