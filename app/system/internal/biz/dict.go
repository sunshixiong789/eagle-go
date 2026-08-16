package biz

import (
	"context"

	"github.com/eagle-go/eagle/app/system/internal/domain"
)

// DictUsecase 是字典模块的应用服务。
//
// 字典没有需要守护的业务不变量，所以这层就是薄薄的编排 + 缓存失效，
// 没有对应的聚合根。战术 DDD 用在有不变量的地方才有价值。
type DictUsecase struct {
	repo domain.DictRepo
}

// NewDictUsecase 构造字典用例。
func NewDictUsecase(repo domain.DictRepo) *DictUsecase {
	return &DictUsecase{repo: repo}
}

// ── 字典类型 ──────────────────────────────────────────────

// CreateDictType 新建字典类型。
func (uc *DictUsecase) CreateDictType(ctx context.Context, t *domain.DictType) (*domain.DictType, error) {
	created, err := uc.repo.CreateType(ctx, t)
	return created, err
}

// ListDictTypes 分页查询字典类型。
func (uc *DictUsecase) ListDictTypes(ctx context.Context, q domain.ListDictTypesQuery) ([]*domain.DictType, int64, error) {
	types, total, err := uc.repo.ListTypes(ctx, q)
	return types, total, err
}

// UpdateDictType 更新字典类型。
// type 字段不可改——它被字典项外键引用，也被前端按名字硬编码引用。
func (uc *DictUsecase) UpdateDictType(ctx context.Context, t *domain.DictType) (*domain.DictType, error) {
	updated, err := uc.repo.UpdateType(ctx, t)
	return updated, err
}

// DeleteDictType 删除字典类型，其下字典项由数据库外键级联删除。
func (uc *DictUsecase) DeleteDictType(ctx context.Context, id int64) error {
	return uc.repo.DeleteType(ctx, id)
}

// ── 字典项 ────────────────────────────────────────────────

// CreateDictData 新建字典项。
func (uc *DictUsecase) CreateDictData(ctx context.Context, d *domain.DictData) (*domain.DictData, error) {
	created, err := uc.repo.CreateData(ctx, d)
	return created, err
}

// ListDictData 分页查询字典项。
func (uc *DictUsecase) ListDictData(ctx context.Context, q domain.ListDictDataQuery) ([]*domain.DictData, int64, error) {
	data, total, err := uc.repo.ListData(ctx, q)
	return data, total, err
}

// GetDictDataByType 取某个类型下的全部启用字典项，走缓存。
func (uc *DictUsecase) GetDictDataByType(ctx context.Context, dictType string) ([]*domain.DictData, error) {
	data, err := uc.repo.ListDataByType(ctx, dictType)
	return data, err
}

// UpdateDictData 更新字典项。字典项不允许跨类型迁移。
func (uc *DictUsecase) UpdateDictData(ctx context.Context, d *domain.DictData) (*domain.DictData, error) {
	current, err := uc.repo.GetDataByID(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	d.DictType = current.DictType

	updated, err := uc.repo.UpdateData(ctx, d)
	return updated, err
}

// DeleteDictData 删除字典项。
func (uc *DictUsecase) DeleteDictData(ctx context.Context, id int64) error {
	return uc.repo.DeleteData(ctx, id)
}
