package biz

import (
	"context"

	"github.com/eagle-go/eagle/app/system/internal/domain"
)

// DictUsecase 是字典模块的应用服务。
//
// 字典没有需要守护的业务不变量，所以这层只保留薄薄的用例编排，
// 没有对应的聚合根。战术 DDD 用在有不变量的地方才有价值。
type DictUsecase struct {
	repo domain.DictRepo
}

func NewDictUsecase(repo domain.DictRepo) *DictUsecase {
	return &DictUsecase{repo: repo}
}

func (uc *DictUsecase) CreateDictType(ctx context.Context, t *domain.DictType) (*domain.DictType, error) {
	return uc.repo.CreateType(ctx, t)
}

func (uc *DictUsecase) ListDictTypes(ctx context.Context, q domain.ListDictTypesQuery) ([]*domain.DictType, int64, error) {
	return uc.repo.ListTypes(ctx, q)
}

// UpdateDictType 更新字典类型。
// type 字段不可改——它被字典项外键引用，也被前端按名字硬编码引用。
func (uc *DictUsecase) UpdateDictType(ctx context.Context, t *domain.DictType) (*domain.DictType, error) {
	return uc.repo.UpdateType(ctx, t)
}

func (uc *DictUsecase) DeleteDictType(ctx context.Context, id int64) error {
	return uc.repo.DeleteType(ctx, id)
}

func (uc *DictUsecase) CreateDictData(ctx context.Context, d *domain.DictData) (*domain.DictData, error) {
	return uc.repo.CreateData(ctx, d)
}

func (uc *DictUsecase) ListDictData(ctx context.Context, q domain.ListDictDataQuery) ([]*domain.DictData, int64, error) {
	return uc.repo.ListData(ctx, q)
}

func (uc *DictUsecase) GetDictDataByType(ctx context.Context, dictType string) ([]*domain.DictData, error) {
	return uc.repo.ListDataByType(ctx, dictType)
}

// UpdateDictData 更新字典项。字典项不允许跨类型迁移。
func (uc *DictUsecase) UpdateDictData(ctx context.Context, d *domain.DictData) (*domain.DictData, error) {
	current, err := uc.repo.GetDataByID(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	d.DictType = current.DictType
	return uc.repo.UpdateData(ctx, d)
}

func (uc *DictUsecase) DeleteDictData(ctx context.Context, id int64) error {
	return uc.repo.DeleteData(ctx, id)
}
