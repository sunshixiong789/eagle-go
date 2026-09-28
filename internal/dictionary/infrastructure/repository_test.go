package infrastructure

import (
	"context"
	"errors"
	"testing"

	"github.com/eagle-go/eagle/internal/dictionary/domain"
)

func skipIfShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
}

func TestDictRepoListByTypeReturnsEnabledItems(t *testing.T) {
	skipIfShort(t)
	items, err := NewDictRepo(testDB).ListDataByType(context.Background(), "sys_common_status")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("种子数据应包含通用状态字典项")
	}
	for _, item := range items {
		if !item.Status.Enabled() {
			t.Fatalf("返回了已停用字典项: %#v", item)
		}
	}
}

func TestDictRepoTypeCRUD(t *testing.T) {
	skipIfShort(t)
	ctx := context.Background()
	repo := NewDictRepo(testDB)
	created, err := repo.CreateType(ctx, &domain.DictType{Name: "测试字典", Type: "test_dict_crud", Status: domain.StatusEnabled})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.DeleteType(ctx, created.ID) })
	_, err = repo.CreateType(ctx, &domain.DictType{Name: "重复", Type: "test_dict_crud", Status: domain.StatusEnabled})
	if !errors.Is(err, domain.ErrDictTypeDuplicated) {
		t.Fatalf("重复类型错误 = %v", err)
	}
}

func TestDictRepoDataRejectsUnknownType(t *testing.T) {
	skipIfShort(t)
	_, err := NewDictRepo(testDB).CreateData(context.Background(), &domain.DictData{
		DictType: "no_such_dict_type", Label: "孤儿项", Value: "x", Status: domain.StatusEnabled,
	})
	if !errors.Is(err, domain.ErrDictTypeNotFound) {
		t.Fatalf("不存在的字典类型错误 = %v", err)
	}
}

func TestDictRepoTypeUpdateAndPagination(t *testing.T) {
	skipIfShort(t)
	ctx := context.Background()
	repo := NewDictRepo(testDB)
	const prefix = "review_type_"
	var created []*domain.DictType
	for _, key := range []string{"alpha", "beta", "gamma"} {
		row, err := repo.CreateType(ctx, &domain.DictType{Name: prefix + key, Type: prefix + key, Status: domain.StatusEnabled})
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, row)
		t.Cleanup(func() { _ = repo.DeleteType(ctx, row.ID) })
	}
	updated, err := repo.UpdateType(ctx, domain.UpdateDictType{ID: created[1].ID, Name: prefix + "renamed", Status: domain.StatusDisabled, Remark: "updated"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Type != created[1].Type || updated.Name != prefix+"renamed" || updated.Status != domain.StatusDisabled || updated.Remark != "updated" {
		t.Fatalf("update=%+v", updated)
	}
	rows, total, err := repo.ListTypes(ctx, domain.ListDictTypesQuery{Keyword: "REVIEW_TYPE_", Offset: 1, PageSize: 1})
	if err != nil || total != 3 || len(rows) != 1 || rows[0].ID != created[1].ID {
		t.Fatalf("page=%+v total=%d error=%v", rows, total, err)
	}
	disabled := domain.StatusDisabled
	rows, total, err = repo.ListTypes(ctx, domain.ListDictTypesQuery{Keyword: prefix, Status: &disabled, PageSize: 20})
	if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != updated.ID {
		t.Fatalf("filter=%+v total=%d error=%v", rows, total, err)
	}
	rows, total, err = repo.ListTypes(ctx, domain.ListDictTypesQuery{Keyword: prefix, Offset: 3, PageSize: 1})
	if err != nil || total != 3 || len(rows) != 0 {
		t.Fatalf("empty page=%+v total=%d error=%v", rows, total, err)
	}
	if err := repo.DeleteType(ctx, updated.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdateType(ctx, domain.UpdateDictType{ID: updated.ID, Name: "missing"}); !errors.Is(err, domain.ErrDictTypeNotFound) {
		t.Fatalf("missing update=%v", err)
	}
	if err := repo.DeleteType(ctx, updated.ID); !errors.Is(err, domain.ErrDictTypeNotFound) {
		t.Fatalf("missing delete=%v", err)
	}
}

func TestDictRepoDataUpdateFilteringAndCascade(t *testing.T) {
	skipIfShort(t)
	ctx := context.Background()
	repo := NewDictRepo(testDB)
	const key = "review_data_crud"
	parent, err := repo.CreateType(ctx, &domain.DictType{Name: "data test", Type: key, Status: domain.StatusEnabled})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.DeleteType(ctx, parent.ID) })
	var items []*domain.DictData
	for i, value := range []string{"alpha", "beta", "gamma"} {
		item, err := repo.CreateData(ctx, &domain.DictData{DictType: key, Label: "Label " + value, Value: value, Sort: int32(3 - i), Status: domain.StatusEnabled})
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, item)
	}
	if _, err := repo.CreateData(ctx, &domain.DictData{DictType: key, Label: "duplicate", Value: "alpha"}); !errors.Is(err, domain.ErrDictDataDuplicated) {
		t.Fatalf("duplicate create=%v", err)
	}
	update := domain.UpdateDictData{ID: items[1].ID, Label: "Edited Beta", Value: "beta-new", Sort: 1, CSSClass: "warning", IsDefault: true, Status: domain.StatusDisabled, Remark: "changed"}
	updated, err := repo.UpdateData(ctx, update)
	if err != nil {
		t.Fatal(err)
	}
	if updated.DictType != key || updated.Label != update.Label || updated.Value != update.Value || updated.Sort != 1 || updated.CSSClass != "warning" || !updated.IsDefault || updated.Status != domain.StatusDisabled || updated.Remark != "changed" {
		t.Fatalf("updated=%+v", updated)
	}
	update.Value = "alpha"
	if _, err := repo.UpdateData(ctx, update); !errors.Is(err, domain.ErrDictDataDuplicated) {
		t.Fatalf("duplicate update=%v", err)
	}
	disabled := domain.StatusDisabled
	rows, total, err := repo.ListData(ctx, domain.ListDictDataQuery{DictType: strptr(key), Keyword: "EDITED", Status: &disabled, PageSize: 20})
	if err != nil || total != 1 || len(rows) != 1 || rows[0].Value != "beta-new" {
		t.Fatalf("filter or failed update changed data: rows=%+v total=%d error=%v", rows, total, err)
	}
	rows, total, err = repo.ListData(ctx, domain.ListDictDataQuery{DictType: strptr(key), Offset: 1, PageSize: 1})
	// beta 与 gamma 的 sort 相同，ID 决定稳定顺序，因此第二页应是 gamma。
	if err != nil || total != 3 || len(rows) != 1 || rows[0].ID != items[2].ID {
		t.Fatalf("page=%+v total=%d error=%v", rows, total, err)
	}
	enabled, err := repo.ListDataByType(ctx, key)
	if err != nil || len(enabled) != 2 || enabled[0].ID != items[2].ID || enabled[1].ID != items[0].ID {
		t.Fatalf("enabled=%+v error=%v", enabled, err)
	}
	if err := repo.DeleteData(ctx, items[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteData(ctx, items[0].ID); !errors.Is(err, domain.ErrDictDataNotFound) {
		t.Fatalf("missing delete=%v", err)
	}
	if _, err := repo.UpdateData(ctx, domain.UpdateDictData{ID: items[0].ID, Label: "missing", Value: "missing"}); !errors.Is(err, domain.ErrDictDataNotFound) {
		t.Fatalf("missing update=%v", err)
	}
	if err := repo.DeleteType(ctx, parent.ID); err != nil {
		t.Fatal(err)
	}
	rows, total, err = repo.ListData(ctx, domain.ListDictDataQuery{DictType: strptr(key), PageSize: 20})
	if err != nil || total != 0 || len(rows) != 0 {
		t.Fatalf("cascade left data=%+v total=%d error=%v", rows, total, err)
	}
}

func strptr(s string) *string { return &s }
