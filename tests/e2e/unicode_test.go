package e2e

import (
	"encoding/json"
	"maps"
	"net/http"
	"strings"
	"testing"
)

// TestUnicodeTextLimits 验证 API 字符数上限与真实存储一致，同时覆盖多字节汉字和四字节字符。
func TestUnicodeTextLimits(t *testing.T) {
	env := newTestEnv(t)
	token := userToken(t, "unicode-user", "admin")
	for _, tc := range []struct {
		name     string
		path     string
		response string
		fields   map[string]int
		base     map[string]any
	}{
		{
			name: "permission", path: "/v1/system/permissions", response: "permission",
			fields: map[string]int{"name": 64, "path": 255, "component": 255, "icon": 64},
			base:   map[string]any{"type": 2, "status": 1},
		},
		{
			name: "dictionary type", path: "/v1/system/dict/types", response: "dict_type",
			fields: map[string]int{"name": 64},
			base:   map[string]any{"type": "test_unicode_limits", "status": 1},
		},
		{
			name: "dictionary data", path: "/v1/system/dict/data", response: "dict_data",
			fields: map[string]int{"label": 128, "value": 128, "css_class": 64},
			base:   map[string]any{"dict_type": "sys_common_status", "status": 1},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := maps.Clone(tc.base)
			for field, limit := range tc.fields {
				request[field] = strings.Repeat("汉", limit)
			}
			body, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			code, response := env.do(t, http.MethodPost, tc.path, token, string(body))
			if code != http.StatusOK {
				t.Fatalf("create at character limit = %d: %s", code, response)
			}
			var result map[string]map[string]any
			decoder := json.NewDecoder(strings.NewReader(response))
			decoder.UseNumber()
			if err := decoder.Decode(&result); err != nil {
				t.Fatal(err)
			}
			id, ok := result[tc.response]["id"].(json.Number)
			if !ok || id == "" {
				t.Fatalf("response has no ID: %s", response)
			}
			itemPath := tc.path + "/" + id.String()
			t.Cleanup(func() {
				if code, body := env.do(t, http.MethodDelete, itemPath, token, ""); code != http.StatusOK {
					t.Errorf("cleanup = %d: %s", code, body)
				}
			})
			for field := range tc.fields {
				if result[tc.response][field] != request[field] {
					t.Errorf("created %s was not preserved", field)
				}
			}

			for field, limit := range tc.fields {
				request[field] = strings.Repeat("😀", limit)
			}
			// 所属字典类型在更新接口中不可变，不能传入更新请求。
			delete(request, "dict_type")
			if tc.response == "dict_type" {
				delete(request, "type")
			}
			body, err = json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			code, response = env.do(t, http.MethodPut, itemPath, token, string(body))
			if code != http.StatusOK {
				t.Fatalf("update at character limit = %d: %s", code, response)
			}
			if err := json.Unmarshal([]byte(response), &result); err != nil {
				t.Fatal(err)
			}
			for field := range tc.fields {
				if result[tc.response][field] != request[field] {
					t.Errorf("updated %s was not preserved", field)
				}
			}

			for field, limit := range tc.fields {
				t.Run("overlong "+field, func(t *testing.T) {
					invalid := maps.Clone(request)
					invalid[field] = strings.Repeat("😀", limit+1)
					body, err := json.Marshal(invalid)
					if err != nil {
						t.Fatal(err)
					}
					if code, response := env.do(t, http.MethodPut, itemPath, token, string(body)); code != http.StatusBadRequest {
						t.Fatalf("overlong %s = %d: %s", field, code, response)
					}
				})
			}
		})
	}
}
