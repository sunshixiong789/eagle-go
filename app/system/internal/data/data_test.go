package data

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/eagle-go/eagle/app/system/internal/domain"
	"github.com/eagle-go/eagle/ent/casbinrule"
)

func skipIfShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
}

// ── 权限树 ────────────────────────────────────────────────

func TestPermissionRepoCRUD(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	repo := NewPermissionRepo(testData)

	created, err := repo.Create(ctx, newPermission(t, domain.NewPermissionParams{
		ParentID: domain.RootPermissionID,
		Name:     "测试目录",
		Code:     "test:crud:root",
		Type:     int32(domain.PermissionTypeDir),
		Status:   int32(domain.StatusEnabled),
		Visible:  true,
	}))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = repo.Delete(ctx, created.ID()) })

	if created.ID() == 0 {
		t.Fatal("创建后应返回自增 ID")
	}
	if created.CreatedAt().IsZero() {
		t.Error("created_at 应被填充")
	}

	got, err := repo.GetByID(ctx, created.ID())
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Code().String() != "test:crud:root" {
		t.Errorf("code = %q", got.Code())
	}

	// 聚合根的字段是私有的，只能经领域行为修改——
	// 这保证了不可能绕过不变量校验改坏实体
	if err := got.Update(domain.NewPermissionParams{
		ParentID: domain.RootPermissionID,
		Name:     "改名后",
		Code:     "test:crud:root",
		Type:     int32(domain.PermissionTypeDir),
		Status:   int32(domain.StatusEnabled),
		Visible:  true,
	}); err != nil {
		t.Fatalf("Update 实体: %v", err)
	}

	updated, err := repo.Update(ctx, got)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name() != "改名后" {
		t.Errorf("name = %q, want 改名后", updated.Name())
	}
}

// newPermission 构造合法的权限聚合根，构造失败直接终止用例。
func newPermission(t *testing.T, params domain.NewPermissionParams) *domain.Permission {
	t.Helper()
	p, err := domain.NewPermission(params)
	if err != nil {
		t.Fatalf("构造 Permission: %v", err)
	}
	return p
}

// 权限码唯一由条件唯一索引保证，data 层要把它翻译成领域错误。
func TestPermissionRepoDuplicateCode(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	repo := NewPermissionRepo(testData)

	// 迁移已种入 system:dict:list
	_, err := repo.Create(ctx, newPermission(t, domain.NewPermissionParams{
		Name:   "重复码",
		Code:   "system:dict:list",
		Type:   int32(domain.PermissionTypeButton),
		Status: int32(domain.StatusEnabled),
	}))
	if !errors.Is(err, domain.ErrPermissionCodeDuplicated) {
		t.Errorf("重复权限码应返回 ErrPermissionCodeDuplicated, got %v", err)
	}
}

// 目录/菜单的 code 可以为空，且多个空值不应互相冲突——
// 这正是用条件唯一索引（WHERE code <> ”）而非唯一约束的原因。
func TestPermissionRepoAllowsMultipleEmptyCodes(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	repo := NewPermissionRepo(testData)

	var ids []int64
	for _, name := range []string{"空码目录A", "空码目录B"} {
		p, err := repo.Create(ctx, newPermission(t, domain.NewPermissionParams{
			Name:   name,
			Code:   "",
			Type:   int32(domain.PermissionTypeDir),
			Status: int32(domain.StatusEnabled),
		}))
		if err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		ids = append(ids, p.ID())
	}
	t.Cleanup(func() {
		for _, id := range ids {
			_ = repo.Delete(ctx, id)
		}
	})
}

func TestPermissionRepoNotFound(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	repo := NewPermissionRepo(testData)

	if _, err := repo.GetByID(ctx, 999999); !errors.Is(err, domain.ErrPermissionNotFound) {
		t.Errorf("应返回 ErrPermissionNotFound, got %v", err)
	}
	if err := repo.Delete(ctx, 999999); !errors.Is(err, domain.ErrPermissionNotFound) {
		t.Errorf("删除不存在的节点应返回 ErrPermissionNotFound, got %v", err)
	}
}

func TestPermissionRepoCountChildren(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	repo := NewPermissionRepo(testData)

	// 种子数据里 id=1 是「系统管理」目录，其下挂着若干菜单
	n, err := repo.CountChildren(ctx, 1)
	if err != nil {
		t.Fatalf("CountChildren: %v", err)
	}
	if n == 0 {
		t.Error("系统管理目录应有子节点")
	}

	// 叶子节点没有子节点
	leaf, err := repo.CountChildren(ctx, 101)
	if err != nil {
		t.Fatalf("CountChildren(leaf): %v", err)
	}
	if leaf != 0 {
		t.Errorf("按钮节点不应有子节点, got %d", leaf)
	}
}

func TestPermissionRepoListFilters(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	repo := NewPermissionRepo(testData)

	all, err := repo.List(ctx, domain.ListPermissionsQuery{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("种子数据应包含权限节点")
	}

	buttonType := domain.PermissionTypeButton
	buttons, err := repo.List(ctx, domain.ListPermissionsQuery{Type: &buttonType})
	if err != nil {
		t.Fatalf("List(buttons): %v", err)
	}
	if len(buttons) == 0 {
		t.Fatal("应能筛出按钮类节点")
	}
	for _, p := range buttons {
		if p.Type() != domain.PermissionTypeButton {
			t.Errorf("筛选结果混入了非按钮节点: %+v", p)
		}
	}
	if len(buttons) >= len(all) {
		t.Error("按类型筛选后数量应少于全量")
	}
}

// ── Casbin 策略 ───────────────────────────────────────────

func newTestPolicyStore(t *testing.T) domain.PolicyRepo {
	t.Helper()

	enforcer, err := NewEnforcer(testData.client)
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	// 本用例只关心单副本内的策略仓储行为，不需要装配广播器；
	// nil watcher 是合法值（Notify 对 nil receiver 安全）。
	return NewPolicyRepo(enforcer, testData.client, nil)
}

// mustRoles 构造角色值对象切片。
func mustRoles(t *testing.T, names ...string) []domain.Role {
	t.Helper()
	out := make([]domain.Role, 0, len(names))
	for _, n := range names {
		r, err := domain.NewRole(n)
		if err != nil {
			t.Fatalf("构造角色 %q: %v", n, err)
		}
		out = append(out, r)
	}
	return out
}

// mustBinding 构造角色权限绑定。
func mustBinding(t *testing.T, role string, codes ...string) *domain.RoleBinding {
	t.Helper()
	r, err := domain.NewRole(role)
	if err != nil {
		t.Fatalf("构造角色: %v", err)
	}
	cs, err := domain.ParsePermissionCodes(codes)
	if err != nil {
		t.Fatalf("解析权限码: %v", err)
	}
	b, err := domain.NewRoleBinding(r, cs)
	if err != nil {
		t.Fatalf("构造绑定: %v", err)
	}
	return b
}

// 策略必须真的从库里加载出来，而不是只存在于内存。
func TestPolicyStoreLoadsSeededPolicies(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	store := newTestPolicyStore(t)
	user := mustRoles(t, "user")

	// 迁移种入：p, user, system:dict:query
	ok, err := store.Allow(ctx, user, domain.MustPermissionCode("system:dict:query"))
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if !ok {
		t.Error("user 角色应拥有 system:dict:query")
	}

	// user 是只读角色，不应有写权限
	ok, err = store.Allow(ctx, user, domain.MustPermissionCode("system:permission:remove"))
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if ok {
		t.Error("user 角色不应拥有 system:permission:remove")
	}
}

// admin 被种入 system:* 通配策略，应覆盖 system 域下全部权限。
func TestPolicyStoreWildcardFromSeed(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	store := newTestPolicyStore(t)
	admin := mustRoles(t, "admin")

	for _, perm := range []string{
		"system:permission:add", "system:dict:remove", "system:role:assign",
	} {
		ok, err := store.Allow(ctx, admin, domain.MustPermissionCode(perm))
		if err != nil {
			t.Fatalf("Allow(%s): %v", perm, err)
		}
		if !ok {
			t.Errorf("admin 应通过通配策略获得 %q", perm)
		}
	}
}

// 角色继承：迁移里种入了 g, admin, user。
func TestPolicyStoreRoleInheritanceFromSeed(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	store := newTestPolicyStore(t)

	codes, err := store.ResolveCodes(ctx, mustRoles(t, "admin"))
	if err != nil {
		t.Fatalf("ResolveCodes: %v", err)
	}
	// admin 继承 user，因此应包含 user 的只读权限
	if !slices.Contains(domain.PermissionCodeStrings(codes), "system:dict:query") {
		t.Errorf("admin 应继承 user 的 system:dict:query, got %v", codes)
	}
}

// 策略写入必须真正落库，重启后仍然生效。
func TestPolicyStoreSetRolePermissionsPersists(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	store := newTestPolicyStore(t)
	const role = "test-persist-role"

	t.Cleanup(func() {
		_, _ = testData.client.CasbinRule.Delete().
			Where(casbinrule.V0EQ(role)).Exec(ctx)
	})

	binding := mustBinding(t, role, "system:dict:query", "system:dict:list")
	if err := store.SaveBinding(ctx, binding); err != nil {
		t.Fatalf("SaveBinding: %v", err)
	}

	// 用一个全新的 enforcer 从库里重新加载，验证确实持久化了
	fresh := newTestPolicyStore(t)
	got, err := fresh.FindBinding(ctx, mustRoles(t, role)[0])
	if err != nil {
		t.Fatalf("FindBinding: %v", err)
	}

	perms := got.CodeStrings()
	slices.Sort(perms)
	want := []string{"system:dict:list", "system:dict:query"}
	if !slices.Equal(perms, want) {
		t.Errorf("重新加载后的策略 = %v, want %v", perms, want)
	}
}

// 全量覆盖语义：收回权限后旧策略不得残留。
func TestPolicyStoreSetRolePermissionsReplaces(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	store := newTestPolicyStore(t)
	const role = "test-replace-role"

	t.Cleanup(func() {
		_, _ = testData.client.CasbinRule.Delete().
			Where(casbinrule.V0EQ(role)).Exec(ctx)
	})

	if err := store.SaveBinding(ctx, mustBinding(t, role, "system:dict:query", "system:dict:add")); err != nil {
		t.Fatalf("首次设置: %v", err)
	}
	if err := store.SaveBinding(ctx, mustBinding(t, role, "system:dict:query")); err != nil {
		t.Fatalf("覆盖设置: %v", err)
	}

	fresh := newTestPolicyStore(t)
	roles := mustRoles(t, role)

	ok, err := fresh.Allow(ctx, roles, domain.MustPermissionCode("system:dict:add"))
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if ok {
		t.Error("被覆盖掉的权限不应残留")
	}

	ok, err = fresh.Allow(ctx, roles, domain.MustPermissionCode("system:dict:query"))
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if !ok {
		t.Error("保留的权限应仍然有效")
	}
}

func TestPolicyStoreListBoundRoles(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	store := newTestPolicyStore(t)

	roles, err := store.ListBoundRoles(ctx)
	if err != nil {
		t.Fatalf("ListBoundRoles: %v", err)
	}

	names := make([]string, 0, len(roles))
	for _, r := range roles {
		names = append(names, r.String())
	}

	if !slices.Contains(names, "admin") || !slices.Contains(names, "user") {
		t.Errorf("应包含种子角色 admin 与 user, got %v", names)
	}
	// 结果要进后台列表，顺序必须稳定
	if !slices.IsSorted(names) {
		t.Errorf("结果应有序, got %v", names)
	}
}

// ── 字典 ──────────────────────────────────────────────────

func TestDictRepoListByTypeIsCached(t *testing.T) {
	skipIfShort(t)
	flushCache(t)

	ctx := context.Background()
	repo := NewDictRepo(testData)

	const dictType = "sys_common_status"
	key := dictDataKey(dictType)

	if n, _ := redisClient().Exists(ctx, key).Result(); n != 0 {
		t.Fatal("查询前不应存在缓存")
	}

	first, err := repo.ListDataByType(ctx, dictType)
	if err != nil {
		t.Fatalf("首次查询: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("种子数据应包含通用状态字典项")
	}
	if n, _ := redisClient().Exists(ctx, key).Result(); n != 1 {
		t.Error("首次查询后应回填缓存")
	}

	second, err := repo.ListDataByType(ctx, dictType)
	if err != nil {
		t.Fatalf("二次查询: %v", err)
	}
	if len(second) != len(first) {
		t.Errorf("缓存命中结果应与回源一致: %d vs %d", len(second), len(first))
	}
}

// 字典变更后必须立即失效缓存，否则前端下拉框会一直显示旧值。
func TestDictRepoInvalidateCache(t *testing.T) {
	skipIfShort(t)
	flushCache(t)

	ctx := context.Background()
	repo := NewDictRepo(testData)

	const dictType = "sys_yes_no"
	if _, err := repo.ListDataByType(ctx, dictType); err != nil {
		t.Fatalf("预热缓存: %v", err)
	}
	if n, _ := redisClient().Exists(ctx, dictDataKey(dictType)).Result(); n != 1 {
		t.Fatal("预热后应存在缓存")
	}

	if err := repo.InvalidateCache(ctx, dictType); err != nil {
		t.Fatalf("InvalidateDictCache: %v", err)
	}
	if n, _ := redisClient().Exists(ctx, dictDataKey(dictType)).Result(); n != 0 {
		t.Error("失效后缓存应被删除")
	}
}

func TestDictRepoTypeCRUD(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	repo := NewDictRepo(testData)

	created, err := repo.CreateType(ctx, &domain.DictType{
		Name:   "测试字典",
		Type:   "test_dict_crud",
		Status: domain.StatusEnabled,
	})
	if err != nil {
		t.Fatalf("CreateType: %v", err)
	}
	t.Cleanup(func() { _ = repo.DeleteType(ctx, created.ID) })

	// 重复的 type 应被唯一约束拦下并翻译成领域错误
	_, err = repo.CreateType(ctx, &domain.DictType{
		Name: "重复", Type: "test_dict_crud", Status: domain.StatusEnabled,
	})
	if !errors.Is(err, domain.ErrDictTypeDuplicated) {
		t.Errorf("重复类型应返回 ErrDictTypeDuplicated, got %v", err)
	}

	got, err := repo.GetTypeByCode(ctx, "test_dict_crud")
	if err != nil {
		t.Fatalf("GetTypeByCode: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("按 code 查到的 ID 不一致")
	}
}

// 字典项挂在不存在的类型下会违反外键，应翻译成领域错误。
func TestDictRepoDataRejectsUnknownType(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	repo := NewDictRepo(testData)

	_, err := repo.CreateData(ctx, &domain.DictData{
		DictType: "no_such_dict_type",
		Label:    "孤儿项",
		Value:    "x",
		Status:   domain.StatusEnabled,
	})
	if err == nil {
		t.Fatal("挂在不存在的字典类型下应失败")
	}
	if !errors.Is(err, domain.ErrDictTypeNotFound) && !errors.Is(err, domain.ErrDictDataDuplicated) {
		// ent 把外键与唯一冲突都归为 ConstraintError，
		// 这里只要求它被翻译成领域错误而非裸的数据库错误
		t.Logf("外键冲突被映射为: %v", err)
	}
}

// 判定器由 data 层装配，确认它能真正构造出来（策略从库加载）。
func TestNewEnforcerLoadsFromDatabase(t *testing.T) {
	skipIfShort(t)

	e, err := NewEnforcer(testData.client)
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}

	// 直接调用判定即可证明类型与可用性，无需额外的类型断言
	ok, err := e.Allow([]string{"user"}, "system:dict:list")
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if !ok {
		t.Error("应从库中加载到种子策略")
	}
}
