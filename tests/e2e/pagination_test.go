package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// TestListPaginationContract 验证统一页码的 HTTP 绑定、相邻页和非法页码。
func TestListPaginationContract(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	const prefix = "review-pagination-"
	for i := range 3 {
		name := fmt.Sprintf("%s%d", prefix, i)
		account, err := env.db.Client().UserAccount.Create().SetID(name).SetDisplayName(name).Save(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = env.db.Client().UserAccount.DeleteOneID(account.ID).Exec(ctx) })
		kind, err := env.db.Client().DictType.Create().SetType(name).SetName(name).Save(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = env.db.Client().DictType.DeleteOneID(kind.ID).Exec(ctx) })
		if _, err := env.db.Client().DictData.Create().SetDictType(name).SetLabel(name).SetValue("entry").Save(ctx); err != nil {
			t.Fatal(err)
		}
	}
	token := env.userToken(t, "pagination-review", "admin")
	for _, route := range []struct {
		path, collection, field string
	}{
		{"/v1/system/accounts", "accounts", "display_name"},
		{"/v1/system/dict/types", "dict_types", "name"},
		{"/v1/system/dict/data", "dict_data", "label"},
	} {
		t.Run(route.collection, func(t *testing.T) {
			base := route.path + "?keyword=" + prefix + "&page_size=1&"
			for _, tc := range []struct {
				query string
				item  int
			}{
				{"page=0", 0}, {"page=1", 1}, {"page=2", 2},
			} {
				code, body := env.get(t, base+tc.query, token)
				if code != http.StatusOK {
					t.Fatalf("%s = %d %s", tc.query, code, body)
				}
				var response map[string]json.RawMessage
				if err := json.Unmarshal([]byte(body), &response); err != nil {
					t.Fatal(err)
				}
				var rows []map[string]any
				if err := json.Unmarshal(response[route.collection], &rows); err != nil {
					t.Fatal(err)
				}
				if len(rows) != 1 || rows[0][route.field] != fmt.Sprintf("%s%d", prefix, tc.item) {
					t.Fatalf("%s rows=%+v", tc.query, rows)
				}
			}
			if code, body := env.get(t, base+"page=-1", token); code != http.StatusBadRequest {
				t.Fatalf("negative page = %d %s", code, body)
			}
		})
	}
}
