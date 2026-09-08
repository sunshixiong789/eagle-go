package domain

import (
	"context"
	"time"
)

// Status 是字典的启用状态，独立于其他模块的状态类型。
type Status int32

const (
	StatusDisabled Status = 0
	StatusEnabled  Status = 1
)

func (s Status) Enabled() bool { return s == StatusEnabled }

// DictType 是字典类型；Type 是创建后不可变的业务键，字典项通过它归属类型。
type DictType struct {
	ID        int64
	Name      string
	Type      string
	Status    Status
	Remark    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// DictData 是字典项实体。
type DictData struct {
	ID        int64
	DictType  string
	Label     string
	Value     string
	Sort      int32
	CSSClass  string
	IsDefault bool
	Status    Status
	Remark    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ListDictTypesQuery 是字典类型的查询条件。
type ListDictTypesQuery struct {
	Keyword  string
	Status   *Status
	Offset   int64
	PageSize int32
}

// ListDictDataQuery 是字典项的查询条件。
type ListDictDataQuery struct {
	DictType *string
	Keyword  string
	Status   *Status
	Offset   int64
	PageSize int32
}

// UpdateDictType 只暴露字典类型允许修改的字段。
type UpdateDictType struct {
	ID     int64
	Name   string
	Status Status
	Remark string
}

// UpdateDictData 只暴露字典项允许修改的字段。
// DictType 创建后不可变，因此不出现在更新参数中。
type UpdateDictData struct {
	ID        int64
	Label     string
	Value     string
	Sort      int32
	CSSClass  string
	IsDefault bool
	Status    Status
	Remark    string
}

// DictRepo 是字典的仓储接口，由基础设施层实现。
type DictRepo interface {
	// CreateType 创建字典类型；Type 重复时返回 ErrDictTypeDuplicated。
	CreateType(ctx context.Context, t *DictType) (*DictType, error)
	// GetTypeByID 查询字典类型；不存在时返回 ErrDictTypeNotFound。
	GetTypeByID(ctx context.Context, id int64) (*DictType, error)
	// ListTypes 按 ID 升序分页返回类型及过滤后的总数，Status 为 nil 时不筛选状态。
	// 总数与当页数据分别查询，不保证来自同一快照。
	ListTypes(ctx context.Context, q ListDictTypesQuery) ([]*DictType, int64, error)
	// UpdateType 更新类型的可变字段；不存在时返回 ErrDictTypeNotFound。
	UpdateType(ctx context.Context, t UpdateDictType) (*DictType, error)
	// DeleteType 删除类型并级联删除其字典项；不存在时返回 ErrDictTypeNotFound。
	DeleteType(ctx context.Context, id int64) error

	// CreateData 创建字典项；类型不存在时返回 ErrDictTypeNotFound，同类型下 Value 重复时返回 ErrDictDataDuplicated。
	CreateData(ctx context.Context, d *DictData) (*DictData, error)
	// GetDataByID 查询字典项；不存在时返回 ErrDictDataNotFound。
	GetDataByID(ctx context.Context, id int64) (*DictData, error)
	// ListData 按类型、Sort、ID 升序分页返回字典项及过滤后的总数，可选条件为 nil 时不筛选。
	// 总数与当页数据分别查询，不保证来自同一快照。
	ListData(ctx context.Context, q ListDictDataQuery) ([]*DictData, int64, error)
	// ListDataByType 按 Sort、ID 升序返回指定类型下已启用的字典项，不检查类型的启用状态。
	// 类型不存在或没有已启用项时返回空列表。
	ListDataByType(ctx context.Context, dictType string) ([]*DictData, error)
	// UpdateData 更新字典项的可变字段；不存在时返回 ErrDictDataNotFound，同类型下 Value 重复时返回 ErrDictDataDuplicated。
	UpdateData(ctx context.Context, update UpdateDictData) (*DictData, error)
	// DeleteData 删除字典项；不存在时返回 ErrDictDataNotFound。
	DeleteData(ctx context.Context, id int64) error
}
