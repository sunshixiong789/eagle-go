package server

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	kratoshttp "github.com/go-kratos/kratos/v3/transport/http"

	"github.com/eagle-go/eagle/pkg/platform/config"
)

//go:embed swagger.html
var swaggerHTML string

var swaggerPath = regexp.MustCompile(`^/[a-zA-Z0-9_-]+(/[a-zA-Z0-9_-]+)*$`)

// RegisterSwagger 在显式启用时注册公开的开发文档页面及 OpenAPI 下载入口。
// 关闭时不读取文件、不注册路由；启用但路径或文件无效时阻止启动。
func RegisterSwagger(srv *kratoshttp.Server, c *config.Server_Swagger) error {
	if !c.GetEnabled() {
		return nil
	}
	base := c.GetPath()
	if !swaggerPath.MatchString(base) || base == "/v1" || strings.HasPrefix(base, "/v1/") {
		return fmt.Errorf("server.swagger.path must be an absolute document path outside /v1, without a trailing slash")
	}
	spec, err := os.ReadFile(c.GetSpecFile())
	if err != nil {
		return fmt.Errorf("server.swagger.spec_file: %w", err)
	}
	var page bytes.Buffer
	tmpl, err := template.New("swagger").Parse(swaggerHTML)
	if err != nil {
		return fmt.Errorf("server.swagger template: %w", err)
	}
	if err := tmpl.Execute(&page, struct{ SpecURL string }{base + "/openapi.yaml"}); err != nil {
		return fmt.Errorf("server.swagger template: %w", err)
	}
	// 文档使用原生 HTTP handler，不经过业务 RPC 鉴权；业务路由仍走原中间件链。
	servePage := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(page.Bytes()))
	}
	srv.HandleFunc(base, servePage)
	srv.HandleFunc(base+"/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		http.ServeContent(w, r, "openapi.yaml", time.Time{}, bytes.NewReader(spec))
	})
	return nil
}
