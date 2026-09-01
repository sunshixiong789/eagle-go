package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

func TestFileLifecycleAndOwnership(t *testing.T) {
	env := newTestEnv(t)
	env.grantRole(t, "file-owner", "system:file:add", "system:file:remove")
	token := env.kc.userToken(t, "file-owner", "file-owner")

	path := "/v1/system/files:upload?filename=" + url.QueryEscape("hello.txt")
	code, _, body := env.doRaw(t, http.MethodPost, path, token, "text/plain", []byte("hello eagle"))
	if code != http.StatusOK {
		t.Fatalf("upload = %d (%s)", code, body)
	}
	var uploaded struct {
		File struct {
			ID     string `json:"id"`
			SHA256 string `json:"sha256"`
			Size   int64  `json:"size"`
		} `json:"file"`
	}
	if err := json.Unmarshal(body, &uploaded); err != nil {
		t.Fatal(err)
	}
	if uploaded.File.ID == "" || uploaded.File.Size != int64(len("hello eagle")) || uploaded.File.SHA256 == "" {
		t.Fatalf("unexpected upload response: %s", body)
	}

	code, header, content := env.doRaw(t, http.MethodGet,
		fmt.Sprintf("/v1/system/files/%s/content", uploaded.File.ID), token, "", nil)
	if code != http.StatusOK || string(content) != "hello eagle" {
		t.Fatalf("download = %d (%q)", code, content)
	}
	if header.Get("Content-Type") != "text/plain" || header.Get("Content-Disposition") != `attachment; filename=hello.txt` {
		t.Fatalf("download headers = %v", header)
	}

	other := env.kc.userToken(t, "other", "file-owner")
	if code, _ := env.get(t, "/v1/system/files/"+uploaded.File.ID, other); code != http.StatusNotFound {
		t.Fatalf("another subject reading file = %d, want 404", code)
	}

	if code, body := env.do(t, http.MethodDelete, "/v1/system/files/"+uploaded.File.ID, token, ""); code != http.StatusOK {
		t.Fatalf("delete = %d (%s)", code, body)
	}
}
