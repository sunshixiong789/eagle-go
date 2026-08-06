package data

import (
	"context"
	"slices"
	"testing"

	"github.com/eagle-go/eagle/app/system/internal/biz"
)

// 权限码查询是鉴权中间件的数据来源，这条链路错了就是越权或误拒。
func TestPermissionCodesFollowRoleBindings(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	resetTables(t)

	ctx := context.Background()
	userRepo := NewUserRepo(testData)

	// 角色 2(common) 由迁移种入，只有 :list / :query 类权限
	u, err := userRepo.Create(ctx, newUser("perm-user"), "h", []int64{2})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	codes, err := userRepo.ListPermissionCodes(ctx, u.ID)
	if err != nil {
		t.Fatalf("ListPermissionCodes: %v", err)
	}
	if len(codes) == 0 {
		t.Fatal("common 角色应带有查询类权限码")
	}

	// 该角色不应拿到任何写权限
	for _, c := range codes {
		switch c {
		case "system:user:add", "system:user:remove", "system:role:add":
			t.Errorf("common 角色不该拥有写权限 %q", c)
		}
	}
	if !slices.Contains(codes, "system:user:list") {
		t.Errorf("应包含 system:user:list, got %v", codes)
	}
}

// 无角色的用户必须拿到空权限集，绝不能因为「查不到」而被当成放行。
func TestPermissionCodesEmptyForUserWithoutRoles(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	resetTables(t)

	ctx := context.Background()
	userRepo := NewUserRepo(testData)

	u, err := userRepo.Create(ctx, newUser("no-role"), "h", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	codes, err := userRepo.ListPermissionCodes(ctx, u.ID)
	if err != nil {
		t.Fatalf("ListPermissionCodes: %v", err)
	}
	if len(codes) != 0 {
		t.Errorf("无角色用户的权限集应为空, got %v", codes)
	}
}

// 权限缓存必须真的写进 Redis，否则每次鉴权都打库。
func TestPermissionCodesAreCached(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	resetTables(t)

	ctx := context.Background()
	userRepo := NewUserRepo(testData)

	u, err := userRepo.Create(ctx, newUser("cache-user"), "h", []int64{2})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	key := userPermKey(u.ID)
	if n, _ := redisClient().Exists(ctx, key).Result(); n != 0 {
		t.Fatal("查询前不应存在缓存")
	}

	first, err := userRepo.ListPermissionCodes(ctx, u.ID)
	if err != nil {
		t.Fatalf("首次查询: %v", err)
	}
	if n, _ := redisClient().Exists(ctx, key).Result(); n != 1 {
		t.Error("首次查询后应回填缓存")
	}

	second, err := userRepo.ListPermissionCodes(ctx, u.ID)
	if err != nil {
		t.Fatalf("二次查询: %v", err)
	}
	if !slices.Equal(first, second) {
		t.Errorf("缓存命中的结果应与回源一致: %v vs %v", first, second)
	}
}

// 收权限后必须立刻生效。若只靠 TTL 自然过期，
// 被撤权的用户在最长一个 TTL 内仍能继续操作——这是安全问题而不只是体验问题。
func TestRevokingRoleInvalidatesCacheImmediately(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	resetTables(t)

	ctx := context.Background()
	userRepo := NewUserRepo(testData)

	u, err := userRepo.Create(ctx, newUser("revoke-user"), "h", []int64{2})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	before, err := userRepo.ListPermissionCodes(ctx, u.ID)
	if err != nil {
		t.Fatalf("撤权前查询: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("撤权前应有权限")
	}

	// 解绑全部角色，并按生产路径主动失效缓存
	if err := userRepo.AssignRoles(ctx, u.ID, nil); err != nil {
		t.Fatalf("AssignRoles: %v", err)
	}
	if err := userRepo.InvalidatePermissionCache(ctx, u.ID); err != nil {
		t.Fatalf("InvalidatePermissionCache: %v", err)
	}

	after, err := userRepo.ListPermissionCodes(ctx, u.ID)
	if err != nil {
		t.Fatalf("撤权后查询: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("撤权后权限集应立即变空, got %v", after)
	}
}

// 角色权限调整后，要能定位到受影响的用户去清缓存。
func TestListUserIDsByRoleDrivesCacheInvalidation(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	resetTables(t)

	ctx := context.Background()
	userRepo := NewUserRepo(testData)
	roleRepo := NewRoleRepo(testData)

	var want []int64
	for _, name := range []string{"member-a", "member-b"} {
		u, err := userRepo.Create(ctx, newUser(name), "h", []int64{2})
		if err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		want = append(want, u.ID)
	}
	// 绑定其他角色的用户不应被牵连
	if _, err := userRepo.Create(ctx, newUser("outsider"), "h", []int64{1}); err != nil {
		t.Fatalf("Create outsider: %v", err)
	}

	got, err := roleRepo.ListUserIDs(ctx, 2)
	if err != nil {
		t.Fatalf("ListUserIDs: %v", err)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("角色 2 的成员 = %v, want %v", got, want)
	}
}

// 权限本身变更时走全量清理（按前缀 SCAN 删除）。
func TestInvalidateAllPermissionCacheClearsEveryUser(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	resetTables(t)

	ctx := context.Background()
	userRepo := NewUserRepo(testData)
	permRepo := NewPermissionRepo(testData)

	for _, name := range []string{"bulk-a", "bulk-b", "bulk-c"} {
		u, err := userRepo.Create(ctx, newUser(name), "h", []int64{2})
		if err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		if _, err := userRepo.ListPermissionCodes(ctx, u.ID); err != nil {
			t.Fatalf("预热缓存: %v", err)
		}
	}

	keys, err := redisClient().Keys(ctx, keyPrefixUserPerm+"*").Result()
	if err != nil {
		t.Fatalf("Keys: %v", err)
	}
	if len(keys) != 3 {
		t.Fatalf("预热后应有 3 个缓存键, got %d", len(keys))
	}

	if err := permRepo.InvalidateAllPermissionCache(ctx); err != nil {
		t.Fatalf("InvalidateAllPermissionCache: %v", err)
	}

	keys, err = redisClient().Keys(ctx, keyPrefixUserPerm+"*").Result()
	if err != nil {
		t.Fatalf("Keys: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("全量清理后不应残留缓存键, got %v", keys)
	}
}

// 权限树：菜单/目录与按钮要能区分开，前端据此渲染路由和按钮。
func TestListPermissionsByUserSeparatesMenusFromButtons(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	resetTables(t)

	ctx := context.Background()
	userRepo := NewUserRepo(testData)
	permRepo := NewPermissionRepo(testData)

	u, err := userRepo.Create(ctx, newUser("menu-user"), "h", []int64{2})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	perms, err := permRepo.ListByUserID(ctx, u.ID)
	if err != nil {
		t.Fatalf("ListByUserID: %v", err)
	}
	if len(perms) == 0 {
		t.Fatal("应能取到该用户的权限节点")
	}

	var menus, buttons int
	for _, p := range perms {
		if p.Type == biz.PermissionTypeButton {
			buttons++
		} else {
			menus++
		}
	}
	if menus == 0 {
		t.Error("应包含菜单或目录节点")
	}
	if buttons == 0 {
		t.Error("common 角色含 :query 按钮权限，应能取到按钮节点")
	}
}
