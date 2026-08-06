package biz

import (
	"context"
	"time"
)

// DictType 是字典类型（一组字典项的容器）。
type DictType struct {
	ID        int64
	Name      string
	Type      string
	Status    int32
	Remark    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// DictData 是字典项。
type DictData struct {
	ID        int64
	DictType  string
	Label     string
	Value     string
	Sort      int32
	CSSClass  string
	IsDefault bool
	Status    int32
	Remark    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ListDictTypesQuery 是字典类型查询条件。
type ListDictTypesQuery struct {
	Keyword  string
	Status   *int32
	Offset   int32
	PageSize int32
}

// ListDictDataQuery 是字典项查询条件。
type ListDictDataQuery struct {
	DictType *string
	Keyword  string
	Status   *int32
	Offset   int32
	PageSize int32
}

// DictRepo 由 data 层实现。
type DictRepo interface {
	CreateType(ctx context.Context, t *DictType) (*DictType, error)
	GetTypeByID(ctx context.Context, id int64) (*DictType, error)
	GetTypeByCode(ctx context.Context, code string) (*DictType, error)
	ListTypes(ctx context.Context, q ListDictTypesQuery) ([]*DictType, int64, error)
	UpdateType(ctx context.Context, t *DictType) (*DictType, error)
	DeleteType(ctx context.Context, id int64) error

	CreateData(ctx context.Context, d *DictData) (*DictData, error)
	GetDataByID(ctx context.Context, id int64) (*DictData, error)
	ListData(ctx context.Context, q ListDictDataQuery) ([]*DictData, int64, error)
	// ListDataByType 是前端下拉框的主要来源，实现方需带缓存
	ListDataByType(ctx context.Context, dictType string) ([]*DictData, error)
	UpdateData(ctx context.Context, d *DictData) (*DictData, error)
	DeleteData(ctx context.Context, id int64) error

	InvalidateDictCache(ctx context.Context, dictTypes ...string) error
}

// DictUsecase 编排字典相关的业务规则。
type DictUsecase struct {
	repo DictRepo
}

// NewDictUsecase 构造字典用例。
func NewDictUsecase(repo DictRepo) *DictUsecase {
	return &DictUsecase{repo: repo}
}

// CreateDictType 新建字典类型。
func (uc *DictUsecase) CreateDictType(ctx context.Context, t *DictType) (*DictType, error) {
	if _, err := uc.repo.GetTypeByCode(ctx, t.Type); err == nil {
		return nil, ErrDictTypeDuplicated
	}
	return uc.repo.CreateType(ctx, t)
}

// ListDictTypes 分页查询字典类型。
func (uc *DictUsecase) ListDictTypes(ctx context.Context, q ListDictTypesQuery) ([]*DictType, int64, error) {
	return uc.repo.ListTypes(ctx, q)
}

// UpdateDictType 更新字典类型。type 字段不可改——它被字典项外键引用，
// 也被前端按名字硬编码引用。
func (uc *DictUsecase) UpdateDictType(ctx context.Context, t *DictType) (*DictType, error) {
	current, err := uc.repo.GetTypeByID(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	updated, err := uc.repo.UpdateType(ctx, t)
	if err != nil {
		return nil, err
	}
	return updated, uc.repo.InvalidateDictCache(ctx, current.Type)
}

// DeleteDictType 删除字典类型。其下字典项由数据库外键级联删除。
func (uc *DictUsecase) DeleteDictType(ctx context.Context, id int64) error {
	current, err := uc.repo.GetTypeByID(ctx, id)
	if err != nil {
		return err
	}
	if err := uc.repo.DeleteType(ctx, id); err != nil {
		return err
	}
	return uc.repo.InvalidateDictCache(ctx, current.Type)
}

// CreateDictData 新建字典项。
func (uc *DictUsecase) CreateDictData(ctx context.Context, d *DictData) (*DictData, error) {
	if _, err := uc.repo.GetTypeByCode(ctx, d.DictType); err != nil {
		return nil, ErrDictTypeNotFound
	}
	created, err := uc.repo.CreateData(ctx, d)
	if err != nil {
		return nil, err
	}
	return created, uc.repo.InvalidateDictCache(ctx, d.DictType)
}

// ListDictData 分页查询字典项。
func (uc *DictUsecase) ListDictData(ctx context.Context, q ListDictDataQuery) ([]*DictData, int64, error) {
	return uc.repo.ListData(ctx, q)
}

// GetDictDataByType 取某个类型下的全部启用字典项，走缓存。
func (uc *DictUsecase) GetDictDataByType(ctx context.Context, dictType string) ([]*DictData, error) {
	return uc.repo.ListDataByType(ctx, dictType)
}

// UpdateDictData 更新字典项。
func (uc *DictUsecase) UpdateDictData(ctx context.Context, d *DictData) (*DictData, error) {
	current, err := uc.repo.GetDataByID(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	d.DictType = current.DictType // 字典项不允许跨类型迁移
	updated, err := uc.repo.UpdateData(ctx, d)
	if err != nil {
		return nil, err
	}
	return updated, uc.repo.InvalidateDictCache(ctx, current.DictType)
}

// DeleteDictData 删除字典项。
func (uc *DictUsecase) DeleteDictData(ctx context.Context, id int64) error {
	current, err := uc.repo.GetDataByID(ctx, id)
	if err != nil {
		return err
	}
	if err := uc.repo.DeleteData(ctx, id); err != nil {
		return err
	}
	return uc.repo.InvalidateDictCache(ctx, current.DictType)
}
