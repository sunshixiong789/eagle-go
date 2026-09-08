package interfaces

import (
	"context"
	"errors"
	"testing"
	"time"

	v1 "github.com/eagle-go/eagle/api/eagle/dictionary/v1"
	"github.com/eagle-go/eagle/internal/dictionary/domain"
)

type dictRepoStub struct {
	domain.DictRepo
	dictType    *domain.DictType
	dictData    *domain.DictData
	typeQuery   domain.ListDictTypesQuery
	dataQuery   domain.ListDictDataQuery
	typeUpdate  domain.UpdateDictType
	dataUpdate  domain.UpdateDictData
	deletedType int64
	deletedData int64
	total       int64
	err         error
}

func (r *dictRepoStub) CreateType(_ context.Context, got *domain.DictType) (*domain.DictType, error) {
	r.dictType = got
	return got, r.err
}
func (r *dictRepoStub) GetTypeByID(context.Context, int64) (*domain.DictType, error) {
	return r.dictType, r.err
}
func (r *dictRepoStub) ListTypes(_ context.Context, q domain.ListDictTypesQuery) ([]*domain.DictType, int64, error) {
	r.typeQuery = q
	return []*domain.DictType{r.dictType}, r.total, r.err
}
func (r *dictRepoStub) UpdateType(_ context.Context, update domain.UpdateDictType) (*domain.DictType, error) {
	r.typeUpdate = update
	return r.dictType, r.err
}
func (r *dictRepoStub) DeleteType(_ context.Context, id int64) error {
	r.deletedType = id
	return r.err
}
func (r *dictRepoStub) CreateData(_ context.Context, got *domain.DictData) (*domain.DictData, error) {
	r.dictData = got
	return got, r.err
}
func (r *dictRepoStub) GetDataByID(context.Context, int64) (*domain.DictData, error) {
	return r.dictData, r.err
}
func (r *dictRepoStub) ListData(_ context.Context, q domain.ListDictDataQuery) ([]*domain.DictData, int64, error) {
	r.dataQuery = q
	return []*domain.DictData{r.dictData}, r.total, r.err
}
func (r *dictRepoStub) ListDataByType(_ context.Context, dictType string) ([]*domain.DictData, error) {
	r.dataQuery.DictType = &dictType
	return []*domain.DictData{r.dictData}, r.err
}
func (r *dictRepoStub) UpdateData(_ context.Context, update domain.UpdateDictData) (*domain.DictData, error) {
	r.dataUpdate = update
	return r.dictData, r.err
}
func (r *dictRepoStub) DeleteData(_ context.Context, id int64) error {
	r.deletedData = id
	return r.err
}

func TestDictionaryConversions(t *testing.T) {
	if toProtoDictType(nil) != nil || toProtoDictData(nil) != nil || ts(time.Time{}) != nil || toStatusPtr(nil) != nil {
		t.Fatal("nil and zero conversions must stay nil")
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	typeModel := &domain.DictType{ID: 1, Name: "性别", Type: "gender", Status: domain.StatusEnabled, Remark: "remark", CreatedAt: now, UpdatedAt: now}
	typeProto := toProtoDictType(typeModel)
	if typeProto.GetId() != 1 || typeProto.GetName() != "性别" || typeProto.GetType() != "gender" ||
		typeProto.GetStatus() != 1 || typeProto.GetRemark() != "remark" || typeProto.GetCreatedAt() == nil || typeProto.GetUpdatedAt() == nil {
		t.Fatalf("dict type = %+v", typeProto)
	}
	dataModel := &domain.DictData{ID: 2, DictType: "gender", Label: "男", Value: "male", Sort: 3, CSSClass: "blue", IsDefault: true, Status: domain.StatusEnabled, Remark: "remark", CreatedAt: now, UpdatedAt: now}
	dataProto := toProtoDictData(dataModel)
	if dataProto.GetId() != 2 || dataProto.GetDictType() != "gender" || dataProto.GetLabel() != "男" ||
		dataProto.GetValue() != "male" || dataProto.GetSort() != 3 || dataProto.GetCssClass() != "blue" ||
		!dataProto.GetIsDefault() || dataProto.GetStatus() != 1 || dataProto.GetRemark() != "remark" ||
		dataProto.GetCreatedAt() == nil || dataProto.GetUpdatedAt() == nil {
		t.Fatalf("dict data = %+v", dataProto)
	}
	value := int32(1)
	if toStatus(value) != domain.StatusEnabled || fromStatus(domain.StatusEnabled) != value || *toStatusPtr(&value) != domain.StatusEnabled {
		t.Fatal("status conversion mismatch")
	}
}

func TestPaginateBoundaries(t *testing.T) {
	for _, tc := range []struct {
		page, size int32
		offset     int64
		limit      int32
	}{
		{0, 0, 0, defaultPageSize},
		{2, -1, 40, defaultPageSize},
		{3, maxPageSize + 1, 600, maxPageSize},
	} {
		if offset, limit := paginate(tc.page, tc.size); offset != tc.offset || limit != tc.limit {
			t.Errorf("paginate(%d,%d) = %d,%d; want %d,%d", tc.page, tc.size, offset, limit, tc.offset, tc.limit)
		}
	}
}

func TestDictTypeService(t *testing.T) {
	repo := &dictRepoStub{total: 1}
	service := NewDictService(repo)
	ctx := context.Background()
	created, err := service.CreateDictType(ctx, &v1.CreateDictTypeRequest{Name: "性别", Type: "gender", Status: 1, Remark: "remark"})
	if err != nil || created.GetDictType().GetName() != "性别" || repo.dictType.Type != "gender" || repo.dictType.Status != domain.StatusEnabled {
		t.Fatalf("create = %+v, model=%+v, err=%v", created, repo.dictType, err)
	}
	status := int32(1)
	listed, err := service.ListDictTypes(ctx, &v1.ListDictTypesRequest{Keyword: "性", Status: &status, Page: 2, PageSize: 10})
	if err != nil || listed.GetTotal() != 1 || len(listed.GetDictTypes()) != 1 ||
		repo.typeQuery.Keyword != "性" || repo.typeQuery.Status == nil || repo.typeQuery.Offset != 20 || repo.typeQuery.PageSize != 10 {
		t.Fatalf("list = %+v, query=%+v, err=%v", listed, repo.typeQuery, err)
	}
	repo.dictType.ID = 7
	updated, err := service.UpdateDictType(ctx, &v1.UpdateDictTypeRequest{Id: 7, Name: "用户性别", Status: 0, Remark: "updated"})
	if err != nil || updated.GetDictType().GetId() != 7 || repo.typeUpdate.ID != 7 ||
		repo.typeUpdate.Name != "用户性别" || repo.typeUpdate.Status != domain.StatusDisabled || repo.typeUpdate.Remark != "updated" {
		t.Fatalf("update = %+v, input=%+v, err=%v", updated, repo.typeUpdate, err)
	}
	if _, err := service.DeleteDictType(ctx, &v1.DeleteDictTypeRequest{Id: 7}); err != nil || repo.deletedType != 7 {
		t.Fatalf("delete id=%d, err=%v", repo.deletedType, err)
	}
}

func TestDictDataService(t *testing.T) {
	repo := &dictRepoStub{total: 1}
	service := NewDictService(repo)
	ctx := context.Background()
	created, err := service.CreateDictData(ctx, &v1.CreateDictDataRequest{
		DictType: "gender", Label: "男", Value: "male", Sort: 3, CssClass: "blue",
		IsDefault: true, Status: 1, Remark: "remark",
	})
	if err != nil || created.GetDictData().GetLabel() != "男" || repo.dictData.DictType != "gender" ||
		repo.dictData.CSSClass != "blue" || !repo.dictData.IsDefault || repo.dictData.Status != domain.StatusEnabled {
		t.Fatalf("create = %+v, model=%+v, err=%v", created, repo.dictData, err)
	}
	status, dictType := int32(1), "gender"
	listed, err := service.ListDictData(ctx, &v1.ListDictDataRequest{
		DictType: &dictType, Keyword: "男", Status: &status, Page: 1, PageSize: 30,
	})
	if err != nil || listed.GetTotal() != 1 || len(listed.GetDictData()) != 1 ||
		repo.dataQuery.DictType == nil || *repo.dataQuery.DictType != dictType || repo.dataQuery.Keyword != "男" ||
		repo.dataQuery.Status == nil || repo.dataQuery.Offset != 30 || repo.dataQuery.PageSize != 30 {
		t.Fatalf("list = %+v, query=%+v, err=%v", listed, repo.dataQuery, err)
	}
	repo.dictData.ID = 8
	updated, err := service.UpdateDictData(ctx, &v1.UpdateDictDataRequest{
		Id: 8, Label: "男性", Value: "M", Sort: 4, CssClass: "primary",
		IsDefault: false, Status: 0, Remark: "updated",
	})
	if err != nil || updated.GetDictData().GetId() != 8 || repo.dataUpdate.ID != 8 || repo.dataUpdate.Label != "男性" ||
		repo.dataUpdate.Value != "M" || repo.dataUpdate.Sort != 4 || repo.dataUpdate.CSSClass != "primary" ||
		repo.dataUpdate.IsDefault || repo.dataUpdate.Status != domain.StatusDisabled || repo.dataUpdate.Remark != "updated" {
		t.Fatalf("update = %+v, input=%+v, err=%v", updated, repo.dataUpdate, err)
	}
	if _, err := service.DeleteDictData(ctx, &v1.DeleteDictDataRequest{Id: 8}); err != nil || repo.deletedData != 8 {
		t.Fatalf("delete id=%d, err=%v", repo.deletedData, err)
	}
	byType, err := service.GetDictDataByType(ctx, &v1.GetDictDataByTypeRequest{DictType: "gender"})
	if err != nil || len(byType.GetDictData()) != 1 || repo.dataQuery.DictType == nil || *repo.dataQuery.DictType != "gender" {
		t.Fatalf("by type = %+v, query=%+v, err=%v", byType, repo.dataQuery, err)
	}
}

func TestDictServicePropagatesRepositoryErrors(t *testing.T) {
	want := errors.New("repository failed")
	repo := &dictRepoStub{err: want}
	service := NewDictService(repo)
	ctx := context.Background()
	checks := []struct {
		name string
		call func() error
	}{
		{"create type", func() error { _, err := service.CreateDictType(ctx, &v1.CreateDictTypeRequest{}); return err }},
		{"list types", func() error { _, err := service.ListDictTypes(ctx, &v1.ListDictTypesRequest{}); return err }},
		{"update type", func() error { _, err := service.UpdateDictType(ctx, &v1.UpdateDictTypeRequest{}); return err }},
		{"delete type", func() error { _, err := service.DeleteDictType(ctx, &v1.DeleteDictTypeRequest{}); return err }},
		{"create data", func() error { _, err := service.CreateDictData(ctx, &v1.CreateDictDataRequest{}); return err }},
		{"list data", func() error { _, err := service.ListDictData(ctx, &v1.ListDictDataRequest{}); return err }},
		{"update data", func() error { _, err := service.UpdateDictData(ctx, &v1.UpdateDictDataRequest{}); return err }},
		{"delete data", func() error { _, err := service.DeleteDictData(ctx, &v1.DeleteDictDataRequest{}); return err }},
		{"data by type", func() error { _, err := service.GetDictDataByType(ctx, &v1.GetDictDataByTypeRequest{}); return err }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.call(); !errors.Is(err, want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
