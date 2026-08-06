package authz

import (
	"context"
	"fmt"
	"sync"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
)

// Enforcer 封装 Casbin，对外只暴露本项目需要的判定与策略管理接口。
//
// 不直接把 *casbin.Enforcer 暴露给中间件和业务层：Casbin 的 API 面很大，
// 收敛成窄接口后，日后换判定引擎（或加数据权限的 ABAC 模型）
// 不会波及调用方。
type Enforcer struct {
	// Casbin 的 Enforcer 自身对策略读写有锁，但 LoadPolicy 期间
	// 会整体替换内存模型，这里再加一层读写锁以保证重载对判定是原子的。
	mu sync.RWMutex
	e  *casbin.Enforcer
}

// NewEnforcer 用给定的策略存储构造判定器。
//
// adapter 传 nil 时策略只存在于内存中，不做持久化。
// 这条路径供测试使用，也适用于策略完全由配置下发、无需运行时修改的部署。
func NewEnforcer(adapter persist.Adapter) (*Enforcer, error) {
	m, err := model.NewModelFromString(ModelText)
	if err != nil {
		return nil, fmt.Errorf("authz: 解析 Casbin 模型: %w", err)
	}

	// 不能写成 casbin.NewEnforcer(m, adapter)：接口值为 nil 时它仍是
	// 非空接口，Casbin 会当成真实 adapter 去调用而 panic
	var e *casbin.Enforcer
	if adapter == nil {
		e, err = casbin.NewEnforcer(m)
	} else {
		e, err = casbin.NewEnforcer(m, adapter)
	}
	if err != nil {
		return nil, fmt.Errorf("authz: 构造 Casbin enforcer: %w", err)
	}

	if adapter != nil {
		// 策略常驻内存，判定不走网络也不查库——鉴权在每个请求的关键路径上。
		// 策略变更由 ReloadPolicy 主动触发。
		if err := e.LoadPolicy(); err != nil {
			return nil, fmt.Errorf("authz: 加载策略: %w", err)
		}
	}

	return &Enforcer{e: e}, nil
}

// Allow 判断任一角色是否被授予了该权限码。
//
// 逐个角色判定而不是把角色集合传进去，是因为 Casbin 的请求模型是单主体的。
// 角色数量通常个位数，遍历成本可忽略。
func (en *Enforcer) Allow(roles []string, perm string) (bool, error) {
	en.mu.RLock()
	defer en.mu.RUnlock()

	for _, role := range roles {
		ok, err := en.e.Enforce(role, perm)
		if err != nil {
			return false, fmt.Errorf("authz: 判定角色 %q 对 %q 的权限: %w", role, perm, err)
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// ReloadPolicy 从存储重新加载全部策略。
// 后台调整角色权限后调用，使变更立即生效而不必重启服务。
func (en *Enforcer) ReloadPolicy(_ context.Context) error {
	en.mu.Lock()
	defer en.mu.Unlock()

	if err := en.e.LoadPolicy(); err != nil {
		return fmt.Errorf("authz: 重载策略: %w", err)
	}
	return nil
}

// SetRolePermissions 全量覆盖某个角色的权限码集合。
//
// 先删后加放在一次调用里完成，避免中间态下该角色短暂没有任何权限。
func (en *Enforcer) SetRolePermissions(_ context.Context, role string, perms []string) error {
	en.mu.Lock()
	defer en.mu.Unlock()

	if _, err := en.e.RemoveFilteredPolicy(0, role); err != nil {
		return fmt.Errorf("authz: 清除角色 %q 的旧策略: %w", role, err)
	}
	if len(perms) == 0 {
		return nil
	}

	rules := make([][]string, 0, len(perms))
	for _, p := range perms {
		rules = append(rules, []string{role, p})
	}
	if _, err := en.e.AddPolicies(rules); err != nil {
		return fmt.Errorf("authz: 写入角色 %q 的策略: %w", role, err)
	}
	return nil
}

// RolePermissions 返回某个角色被直接授予的权限码（不含继承而来的）。
func (en *Enforcer) RolePermissions(_ context.Context, role string) ([]string, error) {
	en.mu.RLock()
	defer en.mu.RUnlock()

	rules, err := en.e.GetFilteredPolicy(0, role)
	if err != nil {
		return nil, fmt.Errorf("authz: 读取角色 %q 的策略: %w", role, err)
	}

	perms := make([]string, 0, len(rules))
	for _, r := range rules {
		if len(r) >= 2 {
			perms = append(perms, r[1])
		}
	}
	return perms, nil
}

// PermissionsOf 汇总若干角色的全部权限码（含继承），结果去重。
// 前端拿它做按钮级显隐。
func (en *Enforcer) PermissionsOf(_ context.Context, roles []string) ([]string, error) {
	en.mu.RLock()
	defer en.mu.RUnlock()

	seen := make(map[string]struct{})
	out := make([]string, 0)

	for _, role := range roles {
		// 用 Implicit 而非 GetFilteredPolicy：后者只返回直接授予的策略，
		// 会漏掉角色继承链上父角色的权限
		implicit, err := en.e.GetImplicitPermissionsForUser(role)
		if err != nil {
			return nil, fmt.Errorf("authz: 展开角色 %q 的权限: %w", role, err)
		}
		for _, rule := range implicit {
			if len(rule) < 2 {
				continue
			}
			if _, dup := seen[rule[1]]; dup {
				continue
			}
			seen[rule[1]] = struct{}{}
			out = append(out, rule[1])
		}
	}
	return out, nil
}

// AddRoleInheritance 建立角色继承：child 继承 parent 的全部权限。
func (en *Enforcer) AddRoleInheritance(_ context.Context, child, parent string) error {
	en.mu.Lock()
	defer en.mu.Unlock()

	if _, err := en.e.AddGroupingPolicy(child, parent); err != nil {
		return fmt.Errorf("authz: 建立 %q -> %q 的继承: %w", child, parent, err)
	}
	return nil
}
