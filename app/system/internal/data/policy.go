package data

import (
	"context"
	"fmt"
	"sort"

	"github.com/eagle-go/eagle/app/system/internal/domain"
	"github.com/eagle-go/eagle/ent"
	"github.com/eagle-go/eagle/ent/casbinrule"
	"github.com/eagle-go/eagle/pkg/authz"
)

// policyRepo 把 Casbin 判定器适配到领域层定义的 PolicyRepo 接口。
//
// 多这一层的意义在于让 domain 与 biz 都不依赖 casbin 包：
// 授权引擎属于基础设施选型，日后换实现或加数据权限的 ABAC 模型时，
// 领域规则与用例编排都不必改动。
type policyRepo struct {
	enforcer *authz.Enforcer
	client   *ent.Client
	// watcher 在策略变更后广播给其它副本，使它们重载内存中的 Casbin 模型。
	// 允许为 nil（Notify 对 nil receiver 安全），单副本场景/部分测试不必装配它。
	watcher *authz.RedisWatcher
}

// NewPolicyRepo 构造策略仓储。
func NewPolicyRepo(enforcer *authz.Enforcer, client *ent.Client, watcher *authz.RedisWatcher) domain.PolicyRepo {
	return &policyRepo{enforcer: enforcer, client: client, watcher: watcher}
}

func (r *policyRepo) Allow(_ context.Context, roles []domain.Role, perm domain.PermissionCode) (bool, error) {
	return r.enforcer.Allow(roleNames(roles), perm.String())
}

func (r *policyRepo) FindBinding(ctx context.Context, role domain.Role) (*domain.RoleBinding, error) {
	raw, err := r.enforcer.RolePermissions(ctx, role.String())
	if err != nil {
		return nil, err
	}

	codes, err := domain.ParsePermissionCodes(raw)
	if err != nil {
		// 库里存着领域层认不出的权限码，说明策略是绕过应用层写进去的。
		// 直接报错而不是静默丢弃——丢弃会让后台显示的权限比实际生效的少。
		return nil, fmt.Errorf("角色 %q 的策略中含非法权限码: %w", role, err)
	}
	return domain.NewRoleBinding(role, codes)
}

func (r *policyRepo) SaveBinding(ctx context.Context, b *domain.RoleBinding) error {
	if err := r.enforcer.SetRolePermissions(ctx, b.Role().String(), b.CodeStrings()); err != nil {
		return err
	}
	r.watcher.Notify(ctx)
	return nil
}

func (r *policyRepo) ResolveCodes(ctx context.Context, roles []domain.Role) ([]domain.PermissionCode, error) {
	raw, err := r.enforcer.PermissionsOf(ctx, roleNames(roles))
	if err != nil {
		return nil, err
	}
	return domain.ParsePermissionCodes(raw)
}

func (r *policyRepo) SaveInheritance(ctx context.Context, ri domain.RoleInheritance) error {
	if err := r.enforcer.AddRoleInheritance(ctx, ri.Child.String(), ri.Parent.String()); err != nil {
		return err
	}
	r.watcher.Notify(ctx)
	return nil
}

// ListBoundRoles 列出已配置过权限的角色。
//
// 直接查库而不是问 Casbin：enforcer 的 GetAllSubjects 返回的是内存模型
// 里的全部主体，包含 g 规则带出的角色，与「配置过 p 策略的角色」语义不同。
// 后台列表要的是后者。
func (r *policyRepo) ListBoundRoles(ctx context.Context) ([]domain.Role, error) {
	rules, err := r.client.CasbinRule.Query().
		Where(casbinrule.PtypeEQ("p")).
		Select(casbinrule.FieldV0).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list bound roles: %w", err)
	}

	seen := make(map[string]struct{}, len(rules))
	names := make([]string, 0, len(rules))
	for _, rule := range rules {
		if rule.V0 == "" {
			continue
		}
		if _, dup := seen[rule.V0]; dup {
			continue
		}
		seen[rule.V0] = struct{}{}
		names = append(names, rule.V0)
	}

	// 结果要进后台列表，顺序稳定才不会每次刷新都跳动
	sort.Strings(names)

	out := make([]domain.Role, 0, len(names))
	for _, n := range names {
		role, err := domain.NewRole(n)
		if err != nil {
			continue
		}
		out = append(out, role)
	}
	return out, nil
}

func roleNames(roles []domain.Role) []string {
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		out = append(out, r.String())
	}
	return out
}
