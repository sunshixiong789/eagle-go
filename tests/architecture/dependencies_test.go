package architecture_test

import (
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestLayerDependencies(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))

	modules := []struct {
		service string
		module  string
	}{
		{service: "admin", module: "access"},
		{service: "admin", module: "dictionary"},
		{service: "admin", module: "file"},
		{service: "admin", module: "notification"},
		{service: "product", module: "product"},
		{service: "order", module: "order"},
	}
	var tests []struct {
		pkg       string
		forbidden []string
		allowed   []string
	}
	for _, module := range modules {
		basePath := "./app/" + module.service + "/internal/" + module.module
		baseImport := "github.com/eagle-go/eagle/app/" + module.service + "/internal/" + module.module
		tests = append(tests,
			struct {
				pkg       string
				forbidden []string
				allowed   []string
			}{
				pkg:       basePath + "/domain",
				forbidden: []string{"github.com/eagle-go/eagle/", "github.com/go-kratos/", "entgo.io/", "github.com/redis/"},
			},
			struct {
				pkg       string
				forbidden []string
				allowed   []string
			}{
				pkg:       basePath + "/application",
				forbidden: []string{"github.com/eagle-go/eagle/", "github.com/go-kratos/", "entgo.io/", "github.com/redis/"},
				allowed:   []string{baseImport + "/domain"},
			},
		)
	}

	for _, tt := range tests {
		t.Run(tt.pkg, func(t *testing.T) {
			cmd := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, tt.pkg)
			cmd.Dir = root
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("go list: %v", err)
			}
			imports := strings.Fields(string(out))
			for _, imp := range imports {
				if slices.Contains(tt.allowed, imp) {
					continue
				}
				for _, prefix := range tt.forbidden {
					if strings.HasPrefix(imp, prefix) {
						t.Errorf("%s must not import %s", tt.pkg, imp)
					}
				}
			}
		})
	}
}

func TestServiceCompositionBoundaries(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	services := []string{"admin", "product", "order"}
	for _, service := range services {
		for _, pkg := range listPackages(t, root, "./app/"+service+"/...") {
			for _, imp := range pkg.Imports {
				const appPrefix = "github.com/eagle-go/eagle/app/"
				if !strings.HasPrefix(imp, appPrefix) {
					continue
				}
				owner := strings.Split(strings.TrimPrefix(imp, appPrefix), "/")[0]
				if owner != service {
					t.Errorf("service %s imports service %s implementation package %s", service, owner, imp)
				}
			}
		}
	}
}

type listedPackage struct {
	ImportPath string
	Imports    []string
}

func listPackages(t *testing.T, root, pattern string) []listedPackage {
	t.Helper()
	cmd := exec.Command("go", "list", "-json", pattern)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list %s: %v", pattern, err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(out)))
	var packages []listedPackage
	for {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decode go list %s: %v", pattern, err)
		}
		packages = append(packages, pkg)
	}
	return packages
}

func TestInfrastructureBoundaries(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))

	for _, pkg := range listPackages(t, root, "./pkg/...") {
		for _, imp := range pkg.Imports {
			if strings.HasPrefix(imp, "github.com/eagle-go/eagle/app/") {
				t.Errorf("shared package %s must not depend on application package %s", pkg.ImportPath, imp)
			}
		}
	}

	for _, service := range []string{"admin", "product", "order"} {
		for _, pkg := range listPackages(t, root, "./app/"+service+"/...") {
			if strings.Contains(pkg.ImportPath, "/infrastructure") ||
				strings.Contains(pkg.ImportPath, "/internal/platform/database") ||
				strings.Contains(pkg.ImportPath, "/tests/") {
				continue
			}
			for _, imp := range pkg.Imports {
				if strings.Contains(imp, "/internal/platform/database/ent") {
					t.Errorf("%s imports persistence package %s outside infrastructure", pkg.ImportPath, imp)
				}
			}
		}
	}
}

// 这些库是 AI 生成代码时最常顺手引进、但本仓库已明确拒绝的。
// 标准库 slices/maps/cmp、encoding/json、errors、log/slog 已经覆盖对应需求。
var bannedModulePrefixes = []string{
	"github.com/samber/lo",
	"github.com/duke-git/lancet",
	"github.com/jinzhu/copier",
	"github.com/mitchellh/mapstructure",
	"github.com/spf13/cast",
	"github.com/pkg/errors",
	"github.com/go-kratos/kratos/v2",
	"github.com/sirupsen/logrus",
	"go.uber.org/zap",
	"gorm.io/gorm",
	"github.com/go-redis/redis/v8",
	"github.com/go-redis/redis/v7",
	"github.com/redis/go-redis",
	"github.com/google/wire",
}

func TestBannedDependencies(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))

	cmd := exec.Command("go", "list", "-m", "-f", "{{if not .Indirect}}{{.Path}}{{end}}", "all")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -m all: %v", err)
	}

	for _, mod := range strings.Fields(string(out)) {
		for _, prefix := range bannedModulePrefixes {
			if mod == prefix || strings.HasPrefix(mod, prefix+"/") {
				t.Errorf("forbidden module %s (matched %s); use the standard library or existing pkg/ instead, see .agents/rules/style.md", mod, prefix)
			}
		}
	}
}
