package authz

import (
	"context"
	"fmt"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"

	"github.com/eagle-go/eagle/ent"
	"github.com/eagle-go/eagle/ent/casbinrule"
	"github.com/eagle-go/eagle/ent/predicate"
)

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
	ctx := context.Background()

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

// SavePolicy 用内存中的策略整体覆盖存储。
//
// 全表删除后重建，因此整个过程必须在事务里完成——
// 中途失败若留下空表，等同于所有人瞬间失去全部权限。
func (a *EntAdapter) SavePolicy(m model.Model) error {
	ctx := context.Background()

	tx, err := a.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("authz: 开启事务: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	if _, err := tx.CasbinRule.Delete().Exec(ctx); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("authz: 清空旧策略: %w", err)
	}

	var builders []*ent.CasbinRuleCreate
	for ptype, ast := range m["p"] {
		for _, rule := range ast.Policy {
			builders = append(builders, newRuleBuilder(tx.Client(), ptype, rule))
		}
	}
	for ptype, ast := range m["g"] {
		for _, rule := range ast.Policy {
			builders = append(builders, newRuleBuilder(tx.Client(), ptype, rule))
		}
	}

	if len(builders) > 0 {
		if _, err := tx.CasbinRule.CreateBulk(builders...).Save(ctx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("authz: 写入新策略: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("authz: 提交事务: %w", err)
	}
	return nil
}

// AddPolicy 新增一条策略。
func (a *EntAdapter) AddPolicy(_ string, ptype string, rule []string) error {
	ctx := context.Background()

	// 唯一索引会拦住重复写入；用 OnConflict 忽略而不是报错，
	// 这样重复调用是幂等的
	err := newRuleBuilder(a.client, ptype, rule).
		OnConflict(conflictTarget()...).
		Ignore().
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("authz: 新增策略: %w", err)
	}
	return nil
}

// conflictTarget 指定 ON CONFLICT 的推断列。
//
// PostgreSQL 要求 ON CONFLICT 必须给出冲突目标（列组合或约束名），
// 不指定会直接报语法错误 42601。这里的列组合必须与迁移中
// uk_casbin_rule 的定义完全一致，否则推断不到该索引。
func conflictTarget() []entsql.ConflictOption {
	return []entsql.ConflictOption{
		entsql.ConflictColumns(
			casbinrule.FieldPtype,
			casbinrule.FieldV0,
			casbinrule.FieldV1,
			casbinrule.FieldV2,
			casbinrule.FieldV3,
			casbinrule.FieldV4,
			casbinrule.FieldV5,
		),
	}
}

// AddPolicies 批量新增策略。
func (a *EntAdapter) AddPolicies(_ string, ptype string, rules [][]string) error {
	if len(rules) == 0 {
		return nil
	}
	ctx := context.Background()

	builders := make([]*ent.CasbinRuleCreate, 0, len(rules))
	for _, rule := range rules {
		builders = append(builders, newRuleBuilder(a.client, ptype, rule))
	}

	err := a.client.CasbinRule.CreateBulk(builders...).
		OnConflict(conflictTarget()...).
		Ignore().
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("authz: 批量新增策略: %w", err)
	}
	return nil
}

// RemovePolicy 删除一条策略。
func (a *EntAdapter) RemovePolicy(_ string, ptype string, rule []string) error {
	ctx := context.Background()

	q := a.client.CasbinRule.Delete().Where(casbinrule.PtypeEQ(ptype))
	for i, v := range rule {
		q = q.Where(fieldEQ(i, v))
	}

	if _, err := q.Exec(ctx); err != nil {
		return fmt.Errorf("authz: 删除策略: %w", err)
	}
	return nil
}

// RemovePolicies 批量删除策略。
//
// 逐条删除放在一个事务里：部分成功会让内存模型与存储不一致，
// 而 Casbin 此时已经按「全部成功」更新了内存，重启后策略会莫名回退。
func (a *EntAdapter) RemovePolicies(_ string, ptype string, rules [][]string) error {
	if len(rules) == 0 {
		return nil
	}
	ctx := context.Background()

	tx, err := a.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("authz: 开启事务: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	for _, rule := range rules {
		q := tx.CasbinRule.Delete().Where(casbinrule.PtypeEQ(ptype))
		for i, v := range rule {
			q = q.Where(fieldEQ(i, v))
		}
		if _, err := q.Exec(ctx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("authz: 批量删除策略: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("authz: 提交事务: %w", err)
	}
	return nil
}

// RemoveFilteredPolicy 按字段位置删除策略。
func (a *EntAdapter) RemoveFilteredPolicy(_ string, ptype string, fieldIndex int, fieldValues ...string) error {
	ctx := context.Background()

	q := a.client.CasbinRule.Delete().Where(casbinrule.PtypeEQ(ptype))
	for i, v := range fieldValues {
		if v == "" {
			// 空串表示该位置不参与过滤，跳过而不是匹配空值——
			// 这是 Casbin 对 RemoveFilteredPolicy 的既定语义
			continue
		}
		q = q.Where(fieldEQ(fieldIndex+i, v))
	}

	if _, err := q.Exec(ctx); err != nil {
		return fmt.Errorf("authz: 按条件删除策略: %w", err)
	}
	return nil
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

// fieldEQ 把 Casbin 的字段序号映射到对应的 ent 谓词。
//
// 序号越界时返回一个恒不匹配的谓词而不是 panic 或忽略：
// 忽略会让本该受限的删除退化成全表删除，宁可删不到也不能误删。
func fieldEQ(index int, value string) predicate.CasbinRule {
	switch index {
	case 0:
		return casbinrule.V0EQ(value)
	case 1:
		return casbinrule.V1EQ(value)
	case 2:
		return casbinrule.V2EQ(value)
	case 3:
		return casbinrule.V3EQ(value)
	case 4:
		return casbinrule.V4EQ(value)
	case 5:
		return casbinrule.V5EQ(value)
	default:
		// 自增主键恒为正，该条件永不成立
		return casbinrule.IDLT(0)
	}
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
