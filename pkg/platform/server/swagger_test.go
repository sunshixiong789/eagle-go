package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	kratoshttp "github.com/go-kratos/kratos/v3/transport/http"

	"github.com/eagle-go/eagle/pkg/platform/config"
)

func TestSwaggerDisabledDoesNotExposeDocuments(t *testing.T) {
	for _, c := range []*config.Server_Swagger{nil, {Path: "/swagger", SpecFile: "missing.yaml"}} {
		srv := kratoshttp.NewServer()
		if err := RegisterSwagger(srv, c); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{"/swagger", "/swagger/", "/swagger/openapi.yaml"} {
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			if w.Code != http.StatusNotFound {
				t.Fatalf("disabled %s: status %d", path, w.Code)
			}
		}
	}
}

func TestSwaggerCustomPathAndSpecification(t *testing.T) {
	spec := "openapi: 3.0.3\ninfo:\n  title: Eagle\n  version: 1\npaths: {}\n"
	file := filepath.Join(t.TempDir(), "openapi.yaml")
	if err := os.WriteFile(file, []byte(spec), 0600); err != nil {
		t.Fatal(err)
	}
	srv := kratoshttp.NewServer()
	if err := RegisterSwagger(srv, &config.Server_Swagger{Enabled: true, Path: "/dev/docs", SpecFile: file}); err != nil {
		t.Fatal(err)
	}
	// 文件只在启动时读取；请求不能借助路径访问磁盘上的其他文件。
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/dev/docs", "/dev/docs/openapi.yaml"} {
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d", path, w.Code)
		}
		if strings.HasSuffix(path, ".yaml") {
			if w.Body.String() != spec || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/yaml") {
				t.Fatalf("unexpected specification response: %s", w.Body.String())
			}
		} else if !strings.Contains(w.Body.String(), `url: "/dev/docs/openapi.yaml"`) {
			t.Fatal("page does not use the configured specification URL")
		}
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/dev/docs/", nil))
	if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/dev/docs" {
		t.Fatalf("trailing slash should redirect to canonical path: %d %s", w.Code, w.Header().Get("Location"))
	}
	for _, path := range []string{"/swagger", "/openapi.yaml", "/dev/docs/config.yaml"} {
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("unexpected exposed route %s: %d", path, w.Code)
		}
	}
}

func TestSwaggerRejectsInvalidConfiguration(t *testing.T) {
	for _, path := range []string{"", "/", "https://example.com/docs", "/docs/", "/docs?x=1", "/docs/{id}", "/v1", "/v1/docs"} {
		if err := RegisterSwagger(kratoshttp.NewServer(), &config.Server_Swagger{Enabled: true, Path: path}); err == nil || !strings.Contains(err.Error(), "server.swagger.path") {
			t.Fatalf("path %q: error %v", path, err)
		}
	}
	if err := RegisterSwagger(kratoshttp.NewServer(), &config.Server_Swagger{Enabled: true, Path: "/swagger", SpecFile: "missing.yaml"}); err == nil || !strings.Contains(err.Error(), "server.swagger.spec_file") {
		t.Fatalf("missing spec: error %v", err)
	}
}
