package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/eagle-go/eagle/internal/platform/database/ent/migrate"
)

// Keep storage ownership explicit. Every generated table must have one owner.
var storageOwners = map[string]struct{ module, model string }{
	"auth_session": {"auth", "AuthSession"}, "user_account": {"auth", "UserAccount"},
	"user_identity": {"auth", "UserIdentity"}, "user_role_binding": {"auth", "UserRoleBinding"},
	"sys_dict_type": {"dictionary", "DictType"}, "sys_dict_data": {"dictionary", "DictData"},
	"permission_definition": {"access", "PermissionDefinition"}, "navigation_node": {"access", "Permission"},
	"permission_tree_state": {"access", "PermissionTreeState"}, "casbin_rule": {"access", "CasbinRule"},
	"authz_policy_state": {"access", "PolicyState"}, "authz_policy_audit": {"access", "PolicyAudit"},
}
var sqlIdentifier = regexp.MustCompile(`[A-Za-z_][A-Za-z_0-9]*`)

func TestTableOwnership(t *testing.T) {
	for _, table := range migrate.Tables {
		if _, ok := storageOwners[table.Name]; !ok {
			t.Errorf("table %s has no module owner", table.Name)
		}
	}
	root := repositoryRoot()
	for _, module := range discoverModules(t, root) {
		err := filepath.WalkDir(filepath.Join(root, "internal", module, "infrastructure"), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			for _, violation := range storageViolations(file, module) {
				t.Errorf("%s: %s", path, violation)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// This guard recognizes literal SQL table names, generated model packages and
// client selectors. Dynamic SQL and indirect aliases still require code review.
func storageViolations(file *ast.File, module string) []string {
	var violations []string
	ast.Inspect(file, func(node ast.Node) bool {
		for table, owner := range storageOwners {
			if owner.module == module {
				continue
			}
			switch n := node.(type) {
			case *ast.BasicLit:
				if n.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(n.Value)
				if err != nil {
					continue
				}
				if strings.Contains(value, "/internal/platform/database/ent/"+strings.ToLower(owner.model)) {
					violations = append(violations, "imports storage model owned by "+owner.module)
					continue
				}
				for _, word := range sqlIdentifier.FindAllString(value, -1) {
					if strings.EqualFold(word, table) {
						violations = append(violations, "references table "+table+" owned by "+owner.module)
						break
					}
				}
			case *ast.SelectorExpr:
				// A shared Ent Client exposes model clients as these fields. Cross-module
				// domain types may use the same name, so limit this check to client chains.
				if n.Sel.Name == owner.model {
					_, call := n.X.(*ast.CallExpr)
					_, chain := n.X.(*ast.SelectorExpr)
					if call || chain {
						violations = append(violations, "references model client "+owner.model+" owned by "+owner.module)
					}
				}
			}
		}
		return true
	})
	return violations
}

func TestStorageGuardRecognizesCrossModuleAccess(t *testing.T) {
	for _, source := range []string{
		"package infrastructure; const query = `SELECT * FROM auth_session`",
		`package infrastructure; import _ "github.com/eagle-go/eagle/internal/platform/database/ent/authsession"`,
		`package infrastructure; func f(){ db.Client().AuthSession.Query() }`,
	} {
		file, err := parser.ParseFile(token.NewFileSet(), "sample.go", source, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(storageViolations(file, "dictionary")) == 0 {
			t.Fatalf("missed forbidden access: %s", source)
		}
		if got := storageViolations(file, "auth"); len(got) != 0 {
			t.Fatalf("rejected owner's access: %v", got)
		}
	}
}
