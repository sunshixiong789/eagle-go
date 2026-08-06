package data

import (
	"context"
	"fmt"
	"sort"

	"github.com/eagle-go/eagle/app/system/internal/biz"
	"github.com/eagle-go/eagle/ent"
	"github.com/eagle-go/eagle/ent/casbinrule"
	"github.com/eagle-go/eagle/pkg/authz"
)

// policyStore 把 Casbin 判定器适配到 biz 层定义的 PolicyStore 接口。
//
// 多这一层的意义在于让 biz 不直接依赖 casbin 包：授权引擎属于基础设施选型，
// 日后换实现或加数据权限的 ABAC 模型时，领域层不受影响。
type policyStore struct {
	enforcer *authz.Enforcer
	client   *ent.Client
}

// NewPolicyStore 构造策略存储适配器。
func NewPolicyStore(enforcer *authz.Enforcer, client *ent.Client) biz.PolicyStore {
	return &policyStore{enforcer: enforcer, client: client}
}

func (s *policyStore) Allow(_ context.Context, roles []string, perm string) (bool, error) {
	return s.enforcer.Allow(roles, perm)
}

func (s *policyStore) RolePermissions(ctx context.Context, role string) ([]string, error) {
	return s.enforcer.RolePermissions(ctx, role)
}

func (s *policyStore) PermissionsOf(ctx context.Context, roles []string) ([]string, error) {
	return s.enforcer.PermissionsOf(ctx, roles)
}

func (s *policyStore) SetRolePermissions(ctx context.Context, role string, perms []string) error {
	return s.enforcer.SetRolePermissions(ctx, role, perms)
}

func (s *policyStore) AddRoleInheritance(ctx context.Context, child, parent string) error {
	return s.enforcer.AddRoleInheritance(ctx, child, parent)
}

// ListBoundRoles 列出已配置过权限的角色。
//
// 直接查库而不是问 Casbin：enforcer 的 GetAllSubjects 返回的是
// 内存模型里的主体，包含 g 规则带出的角色，语义与「配置过 p 策略的角色」
// 不一致。这里要的是后台管理页面能列出来的那一份。
func (s *policyStore) ListBoundRoles(ctx context.Context) ([]string, error) {
	rules, err := s.client.CasbinRule.Query().
		Where(casbinrule.PtypeEQ("p")).
		Select(casbinrule.FieldV0).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list bound roles: %w", err)
	}

	seen := make(map[string]struct{}, len(rules))
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		if r.V0 == "" {
			continue
		}
		if _, dup := seen[r.V0]; dup {
			continue
		}
		seen[r.V0] = struct{}{}
		out = append(out, r.V0)
	}

	// 结果要进后台列表，顺序稳定才不会每次刷新都跳动
	sort.Strings(out)
	return out, nil
}
