package data

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/eagle-go/eagle/app/system/internal/domain"
	"github.com/eagle-go/eagle/ent"
	"github.com/eagle-go/eagle/ent/casbinrule"
	"github.com/eagle-go/eagle/pkg/authz"
	"github.com/eagle-go/eagle/pkg/identity"
	"github.com/go-kratos/kratos/v3/transport"
	"go.opentelemetry.io/otel/trace"
)

// policyRepo 把 Casbin 判定器适配到领域层定义的 PolicyRepo 接口。
//
// 多这一层的意义在于让 domain 与 biz 都不依赖 casbin 包：
// 授权引擎属于基础设施选型，日后换实现或加数据权限的 ABAC 模型时，
// 领域规则与用例编排都不必改动。
type policyRepo struct {
	enforcer *authz.Enforcer
	adapter  *authz.EntAdapter
	client   *ent.Client
	// watcher 在策略变更后广播给其它副本，使它们重载内存中的 Casbin 模型。
	// 允许为 nil（Notify 对 nil receiver 安全），单副本场景/部分测试不必装配它。
	watcher *authz.RedisWatcher
}

// NewPolicyRepo 构造策略仓储。
func NewPolicyRepo(enforcer *authz.Enforcer, client *ent.Client, watcher *authz.RedisWatcher) domain.PolicyRepo {
	adapter, ok := enforcer.EntAdapter()
	if !ok {
		adapter = authz.NewEntAdapter(client)
	}
	return &policyRepo{enforcer: enforcer, adapter: adapter, client: client, watcher: watcher}
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
	binding, err := domain.NewRoleBinding(role, codes)
	if err != nil {
		return nil, err
	}
	version, err := r.adapter.PolicyVersion(ctx)
	if err != nil {
		return nil, err
	}
	return binding.WithRevision(version), nil
}

func (r *policyRepo) SaveBinding(ctx context.Context, b *domain.RoleBinding, expectedVersion *int64) (int64, error) {
	version, err := r.adapter.ReplaceRolePermissionsIfVersion(ctx, b.Role().String(), b.CodeStrings(), expectedVersion, mutationMeta(ctx))
	if err != nil {
		if errors.Is(err, authz.ErrConcurrentModification) {
			return 0, domain.ErrConcurrentModification
		}
		return 0, err
	}
	if err := r.enforcer.ReloadPolicy(ctx); err != nil {
		return version, fmt.Errorf("策略已提交为版本 %d，但本实例重载失败: %w", version, err)
	}
	r.watcher.NotifyVersion(ctx, version)
	return version, nil
}

func (r *policyRepo) ResolveCodes(ctx context.Context, roles []domain.Role) ([]domain.PermissionCode, error) {
	raw, err := r.enforcer.PermissionsOf(ctx, roleNames(roles))
	if err != nil {
		return nil, err
	}
	return domain.ParsePermissionCodes(raw)
}

func (r *policyRepo) SaveInheritance(ctx context.Context, ri domain.RoleInheritance, expectedVersion *int64) (int64, error) {
	version, err := r.adapter.AddRoleInheritanceIfVersion(ctx, ri.Child.String(), ri.Parent.String(), expectedVersion, mutationMeta(ctx))
	if err != nil {
		if errors.Is(err, authz.ErrRoleInheritanceCycle) {
			return 0, domain.ErrRoleInheritanceCycle
		}
		if errors.Is(err, authz.ErrConcurrentModification) {
			return 0, domain.ErrConcurrentModification
		}
		return 0, err
	}
	if err := r.enforcer.ReloadPolicy(ctx); err != nil {
		return version, fmt.Errorf("策略已提交为版本 %d，但本实例重载失败: %w", version, err)
	}
	r.watcher.NotifyVersion(ctx, version)
	return version, nil
}

func (r *policyRepo) PolicyVersion(ctx context.Context) (int64, error) {
	return r.adapter.PolicyVersion(ctx)
}

func (r *policyRepo) ListInheritances(ctx context.Context) ([]domain.RoleInheritance, error) {
	pairs, err := r.adapter.RoleInheritances(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Child == pairs[j].Child {
			return pairs[i].Parent < pairs[j].Parent
		}
		return pairs[i].Child < pairs[j].Child
	})
	out := make([]domain.RoleInheritance, 0, len(pairs))
	for _, pair := range pairs {
		child, err := domain.NewRole(pair.Child)
		if err != nil {
			continue
		}
		parent, err := domain.NewRole(pair.Parent)
		if err != nil {
			continue
		}
		ri, err := domain.NewRoleInheritance(child, parent)
		if err == nil {
			out = append(out, ri)
		}
	}
	return out, nil
}

func (r *policyRepo) DeleteInheritance(ctx context.Context, ri domain.RoleInheritance, expectedVersion *int64) (int64, error) {
	version, err := r.adapter.DeleteRoleInheritanceIfVersion(ctx, ri.Child.String(), ri.Parent.String(), expectedVersion, mutationMeta(ctx))
	if errors.Is(err, authz.ErrConcurrentModification) {
		return 0, domain.ErrConcurrentModification
	}
	if err != nil {
		return 0, err
	}
	if err := r.enforcer.ReloadPolicy(ctx); err != nil {
		return version, fmt.Errorf("策略已提交为版本 %d，但本实例重载失败: %w", version, err)
	}
	r.watcher.NotifyVersion(ctx, version)
	return version, nil
}

func mutationMeta(ctx context.Context) authz.PolicyMutationMeta {
	meta := authz.PolicyMutationMeta{}
	if p, ok := identity.FromContext(ctx); ok {
		meta.ActorSubject = p.Subject
		meta.ActorClientID = p.ClientID
	}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		meta.TraceID = sc.TraceID().String()
	}
	if tr, ok := transport.FromServerContext(ctx); ok {
		meta.RequestID = tr.RequestHeader().Get("X-Request-ID")
	}
	return meta
}

// ListBoundRoles 列出已配置过权限的角色。
//
// 直接查库而不是问 Casbin：enforcer 的 GetAllSubjects 返回的是内存模型
// 里的全部主体，包含 g 规则带出的角色，与「配置过 p 策略的角色」语义不同。
// 后台列表要的是后者。
func (r *policyRepo) ListBoundRoles(ctx context.Context) ([]domain.Role, error) {
	bindings, err := r.ListBindings(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Role, 0, len(bindings))
	for _, binding := range bindings {
		out = append(out, binding.Role())
	}
	return out, nil
}

// ListBindings 用一次数据库查询完成全部角色及其直接权限的分组。
func (r *policyRepo) ListBindings(ctx context.Context) ([]*domain.RoleBinding, error) {
	version, err := r.adapter.PolicyVersion(ctx)
	if err != nil {
		return nil, err
	}
	rules, err := r.client.CasbinRule.Query().
		Where(casbinrule.PtypeEQ("p")).
		Select(casbinrule.FieldV0, casbinrule.FieldV1).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list role bindings: %w", err)
	}

	grouped := make(map[string][]string)
	for _, rule := range rules {
		if rule.V0 == "" || rule.V1 == "" {
			continue
		}
		grouped[rule.V0] = append(grouped[rule.V0], rule.V1)
	}
	names := make([]string, 0, len(grouped))
	for name := range grouped {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]*domain.RoleBinding, 0, len(names))
	for _, n := range names {
		role, err := domain.NewRole(n)
		if err != nil {
			continue
		}
		codes, err := domain.ParsePermissionCodes(grouped[n])
		if err != nil {
			return nil, fmt.Errorf("角色 %q 的策略中含非法权限码: %w", role, err)
		}
		binding, err := domain.NewRoleBinding(role, codes)
		if err != nil {
			return nil, err
		}
		out = append(out, binding.WithRevision(version))
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
