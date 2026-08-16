package authz

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"

	"github.com/eagle-go/eagle/ent"
	"github.com/eagle-go/eagle/ent/casbinrule"
	"github.com/eagle-go/eagle/ent/permissiondefinition"
)

// ErrRoleInheritanceCycle 表示新增的 g 规则会使角色继承图成环。
var ErrRoleInheritanceCycle = errors.New("authz: role inheritance cycle")
var ErrConcurrentModification = errors.New("authz: concurrent policy modification")

// ErrAdapterReadOnly 表示 Casbin 适配器只允许加载策略。
// 生产写入必须走 ReplaceRolePermissions / AddRoleInheritanceAtomic，
// 以便同一事务里递增版本并写审计。
var ErrAdapterReadOnly = errors.New("authz: casbin adapter is load-only")

// EntAdapter 用项目自身的 ent client 持久化 Casbin 策略。
//
// 复用同一个 client 意味着策略表与业务表共享连接池、共享事务能力，
// 也共享同一条 goose 迁移路径——不会出现「业务表迁移到 v5、
// 策略表还停在自动迁移建出来的形态」这种分裂。
type EntAdapter struct {
	client *ent.Client
}

// 编译期确认实现了 Casbin 要求的全部接口。
//
// BatchAdapter 这条尤其重要：Casbin 的 AddPolicies/RemovePolicies 会在运行时
// 把 adapter 强转成 BatchAdapter，少实现一个方法不会有编译错误，
// 而是等到真正批量写策略时才 panic。
var (
	_ persist.Adapter         = (*EntAdapter)(nil)
	_ persist.BatchAdapter    = (*EntAdapter)(nil)
	_ persist.FilteredAdapter = (*EntAdapter)(nil)
)

// NewEntAdapter 构造策略存储。
func NewEntAdapter(client *ent.Client) *EntAdapter {
	return &EntAdapter{client: client}
}

// LoadPolicy 加载全部策略到内存模型。
func (a *EntAdapter) LoadPolicy(m model.Model) error {
	return a.LoadPolicyContext(context.Background(), m)
}

// LoadPolicyContext 是带取消与截止时间的策略加载入口。
func (a *EntAdapter) LoadPolicyContext(ctx context.Context, m model.Model) error {

	rules, err := a.client.CasbinRule.Query().All(ctx)
	if err != nil {
		return fmt.Errorf("authz: 读取策略: %w", err)
	}
	for _, r := range rules {
		if err := loadRule(m, r); err != nil {
			return err
		}
	}
	return nil
}

// PolicyMutationMeta 描述一次策略写入的调用者和关联信息。
type PolicyMutationMeta struct {
	ActorSubject  string
	ActorClientID string
	RequestID     string
	TraceID       string
}

// PolicyVersion 返回数据库中权威策略的单调递增版本。
func (a *EntAdapter) PolicyVersion(ctx context.Context) (int64, error) {
	state, err := a.client.PolicyState.Get(ctx, 1)
	if err != nil {
		return 0, fmt.Errorf("authz: 读取策略版本: %w", err)
	}
	return state.Version, nil
}

// PermissionCatalogCodes 返回权限目录中的全部非空权限码。
func (a *EntAdapter) PermissionCatalogCodes(ctx context.Context) ([]string, error) {
	rows, err := a.client.PermissionDefinition.Query().
		Where(permissiondefinition.StatusEQ(1)).
		Select(permissiondefinition.FieldCode).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("authz: 读取权限目录: %w", err)
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Code)
	}
	return out, nil
}

// ReplaceRolePermissions 在一个事务内全量替换角色权限、递增版本并写入
// 审计。调用成功后数据库永远处于完整的新版本，不存在先删后加
// 失败导致角色权限被清空的中间状态。
func (a *EntAdapter) ReplaceRolePermissions(ctx context.Context, role string, perms []string, meta PolicyMutationMeta) (int64, error) {
	return a.ReplaceRolePermissionsIfVersion(ctx, role, perms, nil, meta)
}

func (a *EntAdapter) ReplaceRolePermissionsIfVersion(ctx context.Context, role string, perms []string, expected *int64, meta PolicyMutationMeta) (int64, error) {
	tx, err := a.client.Tx(ctx)
	if err != nil {
		return 0, fmt.Errorf("authz: 开启角色权限替换事务: %w", err)
	}
	defer rollbackOnPanic(tx)
	if _, err := lockPolicyState(ctx, tx, expected); err != nil {
		_ = tx.Rollback()
		return 0, err
	}

	oldRows, err := tx.CasbinRule.Query().
		Where(casbinrule.PtypeEQ("p"), casbinrule.V0EQ(role)).
		All(ctx)
	if err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("authz: 读取角色 %q 的旧策略: %w", role, err)
	}
	before := make([]string, 0, len(oldRows))
	for _, row := range oldRows {
		before = append(before, row.V1)
	}

	if _, err := tx.CasbinRule.Delete().
		Where(casbinrule.PtypeEQ("p"), casbinrule.V0EQ(role)).
		Exec(ctx); err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("authz: 清除角色 %q 的旧策略: %w", role, err)
	}

	if len(perms) > 0 {
		builders := make([]*ent.CasbinRuleCreate, 0, len(perms))
		for _, perm := range perms {
			builders = append(builders, newRuleBuilder(tx.Client(), "p", []string{role, perm}))
		}
		if _, err := tx.CasbinRule.CreateBulk(builders...).Save(ctx); err != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("authz: 写入角色 %q 的新策略: %w", role, err)
		}
	}

	version, err := recordPolicyMutation(ctx, tx, "replace_role_permissions", role, before, perms, meta)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("authz: 提交角色权限替换事务: %w", err)
	}
	return version, nil
}

// AddRoleInheritanceAtomic 原子地增加角色继承并记录版本和审计。
func (a *EntAdapter) AddRoleInheritanceAtomic(ctx context.Context, child, parent string, meta PolicyMutationMeta) (int64, error) {
	return a.AddRoleInheritanceIfVersion(ctx, child, parent, nil, meta)
}

func (a *EntAdapter) AddRoleInheritanceIfVersion(ctx context.Context, child, parent string, expected *int64, meta PolicyMutationMeta) (int64, error) {
	tx, err := a.client.Tx(ctx)
	if err != nil {
		return 0, fmt.Errorf("authz: 开启角色继承事务: %w", err)
	}
	defer rollbackOnPanic(tx)
	// 先更新单例状态行取得排他锁，使并发的继承图修改串行化。
	state, err := lockPolicyState(ctx, tx, expected)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	grouping, err := tx.CasbinRule.Query().Where(casbinrule.PtypeEQ("g")).All(ctx)
	if err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("authz: 读取角色继承图: %w", err)
	}
	graph := make(map[string][]string)
	for _, rule := range grouping {
		graph[rule.V0] = append(graph[rule.V0], rule.V1)
	}
	if rolePathExists(graph, parent, child) {
		_ = tx.Rollback()
		return 0, ErrRoleInheritanceCycle
	}

	exists, err := tx.CasbinRule.Query().Where(
		casbinrule.PtypeEQ("g"), casbinrule.V0EQ(child), casbinrule.V1EQ(parent),
	).Exist(ctx)
	if err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("authz: 查询角色继承: %w", err)
	}
	if !exists {
		if err := newRuleBuilder(tx.Client(), "g", []string{child, parent}).Exec(ctx); err != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("authz: 写入角色继承: %w", err)
		}
	}
	if exists {
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("authz: 提交幂等角色继承事务: %w", err)
		}
		return state.Version, nil
	}

	after := []string{parent}
	version, err := recordPolicyMutation(ctx, tx, "add_role_inheritance", child, nil, after, meta)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("authz: 提交角色继承事务: %w", err)
	}
	return version, nil
}

// DeleteRoleInheritanceIfVersion 原子删除一条继承关系。
func (a *EntAdapter) DeleteRoleInheritanceIfVersion(ctx context.Context, child, parent string, expected *int64, meta PolicyMutationMeta) (int64, error) {
	tx, err := a.client.Tx(ctx)
	if err != nil {
		return 0, fmt.Errorf("authz: 开启删除角色继承事务: %w", err)
	}
	defer rollbackOnPanic(tx)
	state, err := lockPolicyState(ctx, tx, expected)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	deleted, err := tx.CasbinRule.Delete().Where(
		casbinrule.PtypeEQ("g"), casbinrule.V0EQ(child), casbinrule.V1EQ(parent),
	).Exec(ctx)
	if err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("authz: 删除角色继承: %w", err)
	}
	if deleted == 0 {
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("authz: 提交幂等删除继承事务: %w", err)
		}
		return state.Version, nil
	}
	version, err := recordPolicyMutation(ctx, tx, "delete_role_inheritance", child, []string{parent}, nil, meta)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("authz: 提交删除角色继承事务: %w", err)
	}
	return version, nil
}

type RoleInheritancePair struct{ Child, Parent string }

func (a *EntAdapter) RoleInheritances(ctx context.Context) ([]RoleInheritancePair, error) {
	rows, err := a.client.CasbinRule.Query().Where(casbinrule.PtypeEQ("g")).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("authz: 读取角色继承: %w", err)
	}
	out := make([]RoleInheritancePair, 0, len(rows))
	for _, row := range rows {
		out = append(out, RoleInheritancePair{Child: row.V0, Parent: row.V1})
	}
	return out, nil
}

func lockPolicyState(ctx context.Context, tx *ent.Tx, expected *int64) (*ent.PolicyState, error) {
	state, err := tx.PolicyState.UpdateOneID(1).SetUpdatedAt(time.Now()).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("authz: 锁定策略状态: %w", err)
	}
	if expected != nil && *expected != state.Version {
		return nil, ErrConcurrentModification
	}
	return state, nil
}

func rolePathExists(graph map[string][]string, from, target string) bool {
	seen := make(map[string]struct{})
	stack := []string{from}
	for len(stack) > 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]
		if current == target {
			return true
		}
		if _, ok := seen[current]; ok {
			continue
		}
		seen[current] = struct{}{}
		stack = append(stack, graph[current]...)
	}
	return false
}

func recordPolicyMutation(ctx context.Context, tx *ent.Tx, action, target string, before, after []string, meta PolicyMutationMeta) (int64, error) {
	state, err := tx.PolicyState.UpdateOneID(1).AddVersion(1).SetUpdatedAt(time.Now()).Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("authz: 递增策略版本: %w", err)
	}
	if _, err := tx.PolicyAudit.Create().
		SetPolicyVersion(state.Version).
		SetAction(action).
		SetTarget(target).
		SetActorSubject(meta.ActorSubject).
		SetActorClientID(meta.ActorClientID).
		SetRequestID(meta.RequestID).
		SetTraceID(meta.TraceID).
		SetBefore(before).
		SetAfter(after).
		Save(ctx); err != nil {
		return 0, fmt.Errorf("authz: 写入策略审计: %w", err)
	}
	return state.Version, nil
}

func rollbackOnPanic(tx *ent.Tx) {
	if p := recover(); p != nil {
		_ = tx.Rollback()
		panic(p)
	}
}

// LoadFilteredPolicy 按条件加载策略。
//
// 大多数部署下策略量很小，全量加载即可；保留这个实现是为了
// 将来接入多租户（按域过滤）时不必改判定器的构造方式。
func (a *EntAdapter) LoadFilteredPolicy(m model.Model, filter any) error {
	f, ok := filter.(*Filter)
	if !ok || f == nil {
		return a.LoadPolicy(m)
	}

	ctx := context.Background()
	q := a.client.CasbinRule.Query()
	if f.PType != "" {
		q = q.Where(casbinrule.PtypeEQ(f.PType))
	}
	if f.V0 != "" {
		q = q.Where(casbinrule.V0EQ(f.V0))
	}

	rules, err := q.All(ctx)
	if err != nil {
		return fmt.Errorf("authz: 按条件读取策略: %w", err)
	}
	for _, r := range rules {
		if err := loadRule(m, r); err != nil {
			return err
		}
	}
	return nil
}

// IsFiltered 报告当前是否处于过滤加载模式。
// 本适配器每次都按请求加载，不缓存过滤状态，故恒为 false。
func (a *EntAdapter) IsFiltered() bool { return false }

// Filter 是 LoadFilteredPolicy 的过滤条件。
type Filter struct {
	PType string
	V0    string
}

// SavePolicy 拒绝 Casbin 的全量覆盖写入。
// 策略变更必须走 ReplaceRolePermissions*，否则不会递增版本、也不会写审计。
func (a *EntAdapter) SavePolicy(model.Model) error {
	return fmt.Errorf("authz: SavePolicy: %w", ErrAdapterReadOnly)
}

// AddPolicy 拒绝单条写入。
func (a *EntAdapter) AddPolicy(string, string, []string) error {
	return fmt.Errorf("authz: AddPolicy: %w", ErrAdapterReadOnly)
}

// AddPolicies 拒绝批量写入。
func (a *EntAdapter) AddPolicies(string, string, [][]string) error {
	return fmt.Errorf("authz: AddPolicies: %w", ErrAdapterReadOnly)
}

// RemovePolicy 拒绝单条删除。
func (a *EntAdapter) RemovePolicy(string, string, []string) error {
	return fmt.Errorf("authz: RemovePolicy: %w", ErrAdapterReadOnly)
}

// RemovePolicies 拒绝批量删除。
func (a *EntAdapter) RemovePolicies(string, string, [][]string) error {
	return fmt.Errorf("authz: RemovePolicies: %w", ErrAdapterReadOnly)
}

// RemoveFilteredPolicy 拒绝按条件删除。
func (a *EntAdapter) RemoveFilteredPolicy(string, string, int, ...string) error {
	return fmt.Errorf("authz: RemoveFilteredPolicy: %w", ErrAdapterReadOnly)
}

// ── 内部辅助 ──────────────────────────────────────────────

func loadRule(m model.Model, r *ent.CasbinRule) error {
	line := append([]string{r.Ptype}, trimTrailingEmpty([]string{r.V0, r.V1, r.V2, r.V3, r.V4, r.V5})...)
	if err := persist.LoadPolicyArray(line, m); err != nil {
		return fmt.Errorf("authz: 载入策略 %v: %w", line, err)
	}
	return nil
}

func newRuleBuilder(client *ent.Client, ptype string, rule []string) *ent.CasbinRuleCreate {
	v := make([]string, 6)
	copy(v, rule)
	return client.CasbinRule.Create().
		SetPtype(ptype).
		SetV0(v[0]).SetV1(v[1]).SetV2(v[2]).
		SetV3(v[3]).SetV4(v[4]).SetV5(v[5])
}

// trimTrailingEmpty 去掉尾部的空字段。
// 策略行长度是可变的，把固定 6 列里未使用的空串一并载入会让
// Casbin 认为策略多出几个空参数，导致匹配失败。
func trimTrailingEmpty(vs []string) []string {
	end := len(vs)
	for end > 0 && vs[end-1] == "" {
		end--
	}
	return vs[:end]
}
