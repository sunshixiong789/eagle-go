package architecture_test

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// modulePrefix 是所有业务模块的 import 前缀。
const modulePrefix = "github.com/eagle-go/eagle/internal/"

func TestLayerDependencies(t *testing.T) {
	root := repositoryRoot()
	for _, module := range discoverModules(t, root) {
		basePath := "./internal/" + module
		baseImport := modulePrefix + module

		assertImports(t, root, basePath+"/domain", nil, func(imp string) bool {
			return !isStandardImport(imp)
		})

		if isDirectory(filepath.Join(root, "internal", module, "application")) {
			assertImports(t, root, basePath+"/application", []string{baseImport + "/domain"}, func(imp string) bool {
				return !isStandardImport(imp)
			})
		}

		if isDirectory(filepath.Join(root, "internal", module, "service")) {
			assertImports(t, root, basePath+"/service", nil, func(imp string) bool {
				return strings.HasPrefix(imp, baseImport+"/infrastructure") ||
					strings.Contains(imp, "/internal/platform/database")
			})
		}

		if isDirectory(filepath.Join(root, "internal", module, "infrastructure")) {
			assertImports(t, root, basePath+"/infrastructure/...", nil, func(imp string) bool {
				return strings.HasPrefix(imp, baseImport+"/service")
			})
		}
	}
}

// 单体不代表模块可以互相穿透。模块之间只允许通过对方的 domain 端口或
// application 用例协作：service 是 HTTP 适配层、infrastructure 是持久化细节，
// 被别的模块直接引用就等于把实现绑死，日后想把某个模块拆出去时会寸步难行。
func TestModuleBoundaries(t *testing.T) {
	root := repositoryRoot()
	modules := discoverModules(t, root)

	for _, pkg := range listPackages(t, root, "./internal/...") {
		owner := moduleOf(pkg.ImportPath, modules)
		if owner == "" {
			continue
		}
		for _, imp := range pkg.Imports {
			target := moduleOf(imp, modules)
			if target == "" || target == owner {
				continue
			}
			rest := strings.TrimPrefix(imp, modulePrefix+target+"/")
			if strings.HasPrefix(rest, "service") || strings.HasPrefix(rest, "infrastructure") {
				t.Errorf("模块 %s 直接引用了模块 %s 的实现包 %s（只能经由 domain 端口或 application 用例）",
					owner, target, imp)
			}
		}
	}
}

// moduleOf 返回 import path 所属的业务模块名；不属于任何模块时返回空串。
func moduleOf(importPath string, modules []string) string {
	if !strings.HasPrefix(importPath, modulePrefix) {
		return ""
	}
	name, _, _ := strings.Cut(strings.TrimPrefix(importPath, modulePrefix), "/")
	if !slices.Contains(modules, name) {
		return ""
	}
	return name
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

func repositoryRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// discoverModules 以「有没有 domain 目录」判定业务模块，
// 这样 internal/platform（技术设施，没有领域层）不会被误当成模块。
func discoverModules(t *testing.T, root string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(root, "internal", "*", "domain"))
	if err != nil {
		t.Fatalf("discover modules: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("internal/ 下没有发现任何业务模块，分层测试会变成空跑")
	}
	modules := make([]string, 0, len(paths))
	for _, path := range paths {
		modules = append(modules, filepath.Base(filepath.Dir(path)))
	}
	slices.Sort(modules)
	return modules
}

func assertImports(t *testing.T, root, pkg string, allowed []string, forbidden func(string) bool) {
	t.Helper()
	t.Run(pkg, func(t *testing.T) {
		cmd := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, pkg)
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list: %v", err)
		}
		for _, imp := range strings.Fields(string(out)) {
			if slices.Contains(allowed, imp) {
				continue
			}
			if forbidden(imp) {
				t.Errorf("%s must not import %s", pkg, imp)
			}
		}
	})
}

func isStandardImport(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func TestInfrastructureBoundaries(t *testing.T) {
	root := repositoryRoot()

	// pkg/ 是无业务语义的技术能力层。一旦它 import 了 internal/，
	// 依赖方向就反了：技术设施开始依赖业务，两边再也拆不开。
	for _, pkg := range listPackages(t, root, "./pkg/...") {
		for _, imp := range pkg.Imports {
			if strings.HasPrefix(imp, "github.com/eagle-go/eagle/internal/") {
				t.Errorf("shared package %s must not depend on application package %s", pkg.ImportPath, imp)
			}
		}
	}

	for _, pkg := range listPackages(t, root, "./internal/...") {
		if strings.Contains(pkg.ImportPath, "/infrastructure") ||
			strings.Contains(pkg.ImportPath, "/internal/platform/database") {
			continue
		}
		for _, imp := range pkg.Imports {
			if strings.Contains(imp, "/internal/platform/database/ent") {
				t.Errorf("%s imports persistence package %s outside infrastructure", pkg.ImportPath, imp)
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
}

// Wire 只能出现在组合根。散落到业务包里，依赖关系就从「一处可读的装配清单」
// 退化成到处都是的隐式全局注册。
func TestWireOnlyInCompositionRoot(t *testing.T) {
	root := repositoryRoot()
	for _, pkg := range listPackages(t, root, "./...") {
		if !slices.Contains(pkg.Imports, "github.com/google/wire") {
			continue
		}
		if pkg.ImportPath != "github.com/eagle-go/eagle/cmd/eagle" {
			t.Errorf("%s imports github.com/google/wire; Wire stays in cmd/eagle", pkg.ImportPath)
		}
	}
}

func TestBannedDependencies(t *testing.T) {
	root := repositoryRoot()

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
