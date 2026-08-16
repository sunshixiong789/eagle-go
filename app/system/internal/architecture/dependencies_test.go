package architecture_test

import (
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLayerDependencies(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))

	tests := []struct {
		pkg       string
		forbidden []string
	}{
		{
			pkg:       "./app/system/internal/domain",
			forbidden: []string{"github.com/eagle-go/eagle/", "github.com/go-kratos/", "entgo.io/", "github.com/redis/"},
		},
		{
			pkg:       "./app/system/internal/biz",
			forbidden: []string{"github.com/eagle-go/eagle/api/", "github.com/eagle-go/eagle/ent", "github.com/go-kratos/", "entgo.io/", "github.com/redis/"},
		},
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
				for _, prefix := range tt.forbidden {
					if strings.HasPrefix(imp, prefix) {
						t.Errorf("%s must not import %s", tt.pkg, imp)
					}
				}
			}
		})
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
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))

	for _, pkg := range listPackages(t, root, "./pkg/...") {
		for _, imp := range pkg.Imports {
			if strings.HasPrefix(imp, "github.com/eagle-go/eagle/app/") {
				t.Errorf("shared package %s must not depend on application package %s", pkg.ImportPath, imp)
			}
		}
	}

	for _, pkg := range listPackages(t, root, "./app/system/internal/...") {
		if strings.HasPrefix(pkg.ImportPath, "github.com/eagle-go/eagle/app/system/internal/data") {
			continue
		}
		for _, imp := range pkg.Imports {
			if imp == "github.com/eagle-go/eagle/ent" || strings.HasPrefix(imp, "github.com/eagle-go/eagle/ent/") {
				t.Errorf("%s imports persistence package %s outside data layer", pkg.ImportPath, imp)
			}
		}
	}
}
