package data

import (
	"context"
	"errors"
	"testing"

	"github.com/eagle-go/eagle/app/system/internal/biz"
)

func newUser(username string) *biz.User {
	return &biz.User{
		Username: username,
		Nickname: username + "-nick",
		Email:    username + "@example.com",
		Status:   biz.StatusEnabled,
	}
}

func TestUserRepoCreateAndGet(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	resetTables(t)

	ctx := context.Background()
	repo := NewUserRepo(testData)

	created, err := repo.Create(ctx, newUser("alice"), "hash-placeholder", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("创建后应返回自增 ID")
	}

	got, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Username != "alice" || got.Email != "alice@example.com" {
		t.Errorf("读回的数据不匹配: %+v", got)
	}
	if got.CreatedAt.IsZero() {
		t.Error("created_at 应由数据库默认值填充")
	}
}

// 用户名唯一由条件唯一索引保证，data 层必须把它翻译成领域错误，
// 而不是把裸的 23505 抛给上层。
func TestUserRepoDuplicateUsername(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	resetTables(t)

	ctx := context.Background()
	repo := NewUserRepo(testData)

	if _, err := repo.Create(ctx, newUser("bob"), "h", nil); err != nil {
		t.Fatalf("首次创建: %v", err)
	}

	_, err := repo.Create(ctx, newUser("bob"), "h", nil)
	if !errors.Is(err, biz.ErrUserAlreadyExists) {
		t.Errorf("重复用户名应返回 ErrUserAlreadyExists, got %v", err)
	}
}

func TestUserRepoGetMissingReturnsDomainError(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	resetTables(t)

	_, err := NewUserRepo(testData).GetByID(context.Background(), 999999)
	if !errors.Is(err, biz.ErrUserNotFound) {
		t.Errorf("应返回 ErrUserNotFound, got %v", err)
	}
}

// 软删除后用户查不到，但同名可以重新注册——
// 这正是用条件唯一索引（WHERE deleted_at IS NULL）而非唯一约束的原因。
func TestUserRepoSoftDeleteAllowsUsernameReuse(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	resetTables(t)

	ctx := context.Background()
	repo := NewUserRepo(testData)

	created, err := repo.Create(ctx, newUser("carol"), "h", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := repo.GetByID(ctx, created.ID); !errors.Is(err, biz.ErrUserNotFound) {
		t.Errorf("软删除后应查不到, got %v", err)
	}
	if _, err := repo.Create(ctx, newUser("carol"), "h", nil); err != nil {
		t.Errorf("软删除后同名应可重新注册, got %v", err)
	}
	// 重复删除应被识别为「不存在」而不是静默成功
	if err := repo.Delete(ctx, created.ID); !errors.Is(err, biz.ErrUserNotFound) {
		t.Errorf("重复删除应返回 ErrUserNotFound, got %v", err)
	}
}

// 创建用户 + 绑定角色必须在同一事务内，中途失败不能留下半截数据。
func TestUserRepoCreateWithRolesIsAtomic(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	resetTables(t)

	ctx := context.Background()
	repo := NewUserRepo(testData)

	// 角色 1(admin) 由 00003 种入，999999 不存在 -> 外键失败 -> 整体回滚
	_, err := repo.Create(ctx, newUser("dave"), "h", []int64{1, 999999})
	if !errors.Is(err, biz.ErrRoleNotFound) {
		t.Fatalf("绑定不存在的角色应返回 ErrRoleNotFound, got %v", err)
	}

	// 用户不应被留在库里
	if _, _, err := repo.GetByUsernameWithSecret(ctx, "dave"); !errors.Is(err, biz.ErrUserNotFound) {
		t.Errorf("事务应已回滚，用户不该存在, got %v", err)
	}
}

func TestUserRepoAssignRolesReplacesAll(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	resetTables(t)

	ctx := context.Background()
	repo := NewUserRepo(testData)

	u, err := repo.Create(ctx, newUser("erin"), "h", []int64{1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 覆盖成只有角色 2(common)
	if err := repo.AssignRoles(ctx, u.ID, []int64{2}); err != nil {
		t.Fatalf("AssignRoles: %v", err)
	}

	got, err := repo.GetByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if len(got.RoleIDs) != 1 || got.RoleIDs[0] != 2 {
		t.Errorf("角色应被全量覆盖为 [2], got %v", got.RoleIDs)
	}
	if len(got.RoleCodes) != 1 || got.RoleCodes[0] != "common" {
		t.Errorf("角色码应为 [common], got %v", got.RoleCodes)
	}
}

func TestUserRepoGetByUsernameReturnsSecret(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	resetTables(t)

	ctx := context.Background()
	repo := NewUserRepo(testData)

	const hash = "$argon2id$v=19$m=65536,t=3,p=4$abc$def"
	if _, err := repo.Create(ctx, newUser("frank"), hash, nil); err != nil {
		t.Fatalf("Create: %v", err)
	}

	u, gotHash, err := repo.GetByUsernameWithSecret(ctx, "frank")
	if err != nil {
		t.Fatalf("GetByUsernameWithSecret: %v", err)
	}
	if gotHash != hash {
		t.Errorf("口令哈希 = %q, want %q", gotHash, hash)
	}
	if u.Username != "frank" {
		t.Errorf("username = %q", u.Username)
	}
}

func TestUserRepoListFiltersAndCounts(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	resetTables(t)

	ctx := context.Background()
	repo := NewUserRepo(testData)

	for _, name := range []string{"grace", "grant", "henry"} {
		if _, err := repo.Create(ctx, newUser(name), "h", nil); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}
	disabled := newUser("ivan")
	disabled.Status = biz.StatusDisabled
	if _, err := repo.Create(ctx, disabled, "h", nil); err != nil {
		t.Fatalf("Create ivan: %v", err)
	}

	t.Run("关键字前缀匹配", func(t *testing.T) {
		users, total, err := repo.List(ctx, biz.ListUsersQuery{Keyword: "gra", PageSize: 10})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if total != 2 || len(users) != 2 {
			t.Errorf("grace/grant 应命中 2 条, got total=%d len=%d", total, len(users))
		}
	})

	t.Run("按状态过滤", func(t *testing.T) {
		st := biz.StatusDisabled
		users, total, err := repo.List(ctx, biz.ListUsersQuery{Status: &st, PageSize: 10})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if total != 1 || len(users) != 1 || users[0].Username != "ivan" {
			t.Errorf("停用用户应只有 ivan, got total=%d %+v", total, users)
		}
	})

	t.Run("不过滤时返回全部", func(t *testing.T) {
		_, total, err := repo.List(ctx, biz.ListUsersQuery{PageSize: 10})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if total != 4 {
			t.Errorf("total = %d, want 4", total)
		}
	})

	t.Run("分页", func(t *testing.T) {
		users, total, err := repo.List(ctx, biz.ListUsersQuery{Offset: 2, PageSize: 2})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if total != 4 {
			t.Errorf("total 应为总数而非本页数, got %d", total)
		}
		if len(users) != 2 {
			t.Errorf("本页应有 2 条, got %d", len(users))
		}
	})
}
