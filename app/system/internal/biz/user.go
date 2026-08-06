package biz

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/eagle-go/eagle/pkg/password"
)

// 内置超级管理员的用户名与角色码。停用或删除它会把所有人锁在系统外，
// 因此在领域层硬性拦截，而不是靠前端隐藏按钮。
const (
	BuiltinAdminUsername = "admin"
	BuiltinAdminRoleCode = "admin"
)

// User 是用户领域模型。口令哈希不在其中——它只在 data 层和
// VerifyCredentials 的调用路径上短暂存在，不进入领域对象。
type User struct {
	ID          int64
	Username    string
	Nickname    string
	Email       string
	Phone       string
	DeptID      int64
	Status      int32
	Remark      string
	RoleIDs     []int64
	RoleCodes   []string
	LastLoginAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Enabled 表示账号处于可登录状态。
func (u *User) Enabled() bool { return u != nil && u.Status == StatusEnabled }

// 账号/数据状态取值。
const (
	StatusDisabled int32 = 0
	StatusEnabled  int32 = 1
)

// ListUsersQuery 是用户列表的查询条件。指针字段为 nil 表示不按该维度过滤。
type ListUsersQuery struct {
	Keyword  string
	Status   *int32
	DeptID   *int64
	Offset   int32
	PageSize int32
}

// UserRepo 由 data 层实现。依赖方向由内向外倒置：
// 领域层定义接口，基础设施层适配它。
type UserRepo interface {
	Create(ctx context.Context, u *User, passwordHash string, roleIDs []int64) (*User, error)
	GetByID(ctx context.Context, id int64) (*User, error)
	// GetByUsernameWithSecret 额外返回口令哈希，仅供登录校验使用。
	// 单独开这个方法而不是让 GetByID 带回哈希，是为了让"谁读了哈希"
	// 在代码里一眼可查。
	GetByUsernameWithSecret(ctx context.Context, username string) (*User, string, error)
	List(ctx context.Context, q ListUsersQuery) ([]*User, int64, error)
	Update(ctx context.Context, u *User) (*User, error)
	UpdatePassword(ctx context.Context, id int64, passwordHash string) error
	GetPasswordHash(ctx context.Context, id int64) (string, error)
	Delete(ctx context.Context, id int64) error
	AssignRoles(ctx context.Context, userID int64, roleIDs []int64) error
	TouchLastLogin(ctx context.Context, id int64) error
	// ListPermissionCodes 返回用户的全部权限码，鉴权中间件的数据来源。
	ListPermissionCodes(ctx context.Context, userID int64) ([]string, error)
	// InvalidatePermissionCache 在权限相关变更后清理缓存。
	InvalidatePermissionCache(ctx context.Context, userIDs ...int64) error
}

// UserUsecase 编排用户相关的业务规则。
type UserUsecase struct {
	repo     UserRepo
	roleRepo RoleRepo
}

// NewUserUsecase 构造用户用例。
func NewUserUsecase(repo UserRepo, roleRepo RoleRepo) *UserUsecase {
	return &UserUsecase{repo: repo, roleRepo: roleRepo}
}

// CreateUser 创建用户并绑定角色。用户名唯一性由数据库的条件唯一索引兜底，
// 这里的预查只为给出更友好的错误，不作为并发安全的依据。
func (uc *UserUsecase) CreateUser(ctx context.Context, u *User, plainPassword string, roleIDs []int64) (*User, error) {
	u.Username = strings.TrimSpace(u.Username)
	if u.Username == "" {
		return nil, ErrUserAlreadyExists
	}

	hash, err := password.Hash(plainPassword)
	if err != nil {
		return nil, err
	}
	return uc.repo.Create(ctx, u, hash, roleIDs)
}

// GetUser 按 ID 取用户。
func (uc *UserUsecase) GetUser(ctx context.Context, id int64) (*User, error) {
	return uc.repo.GetByID(ctx, id)
}

// ListUsers 分页查询用户。
func (uc *UserUsecase) ListUsers(ctx context.Context, q ListUsersQuery) ([]*User, int64, error) {
	return uc.repo.List(ctx, q)
}

// UpdateUser 更新用户资料。内置管理员不允许被停用。
func (uc *UserUsecase) UpdateUser(ctx context.Context, u *User) (*User, error) {
	current, err := uc.repo.GetByID(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	if current.Username == BuiltinAdminUsername && u.Status == StatusDisabled {
		return nil, ErrUserProtected
	}
	return uc.repo.Update(ctx, u)
}

// DeleteUser 软删除用户。内置管理员受保护。
func (uc *UserUsecase) DeleteUser(ctx context.Context, id int64) error {
	current, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if current.Username == BuiltinAdminUsername {
		return ErrUserProtected
	}
	if err := uc.repo.Delete(ctx, id); err != nil {
		return err
	}
	// 用户已删除，其权限缓存必须立刻失效，否则存量 token 还能继续通过鉴权
	return uc.repo.InvalidatePermissionCache(ctx, id)
}

// ResetPassword 由管理员重置他人口令，不需要原口令。
func (uc *UserUsecase) ResetPassword(ctx context.Context, id int64, newPassword string) error {
	if _, err := uc.repo.GetByID(ctx, id); err != nil {
		return err
	}
	hash, err := password.Hash(newPassword)
	if err != nil {
		return err
	}
	return uc.repo.UpdatePassword(ctx, id, hash)
}

// ChangeOwnPassword 由用户本人改密，必须校验原口令。
func (uc *UserUsecase) ChangeOwnPassword(ctx context.Context, id int64, oldPassword, newPassword string) error {
	hash, err := uc.repo.GetPasswordHash(ctx, id)
	if err != nil {
		return err
	}
	if err := password.Verify(oldPassword, hash); err != nil {
		return ErrPasswordIncorrect
	}

	newHash, err := password.Hash(newPassword)
	if err != nil {
		return err
	}
	return uc.repo.UpdatePassword(ctx, id, newHash)
}

// AssignRoles 全量覆盖用户的角色绑定，并失效其权限缓存。
func (uc *UserUsecase) AssignRoles(ctx context.Context, userID int64, roleIDs []int64) error {
	if _, err := uc.repo.GetByID(ctx, userID); err != nil {
		return err
	}
	if err := uc.repo.AssignRoles(ctx, userID, roleIDs); err != nil {
		return err
	}
	return uc.repo.InvalidatePermissionCache(ctx, userID)
}

// VerifyCredentials 校验账密，供认证中心通过内部接口调用。
//
// 返回值语义刻意区分两种失败：
//   - ErrPasswordIncorrect：账号不存在或口令错误，对外必须合并成同一句提示
//   - ErrUserDisabled：账密正确但账号被停用，可以如实告知
func (uc *UserUsecase) VerifyCredentials(ctx context.Context, username, plainPassword string) (*User, error) {
	u, hash, err := uc.repo.GetByUsernameWithSecret(ctx, username)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			// 即使用户不存在也要走一次哈希计算，让响应时间与
			// "用户存在但密码错" 保持一致，消除计时侧信道
			_ = password.Verify(plainPassword, dummyHash)
			return nil, ErrPasswordIncorrect
		}
		return nil, err
	}

	if err := password.Verify(plainPassword, hash); err != nil {
		return nil, ErrPasswordIncorrect
	}
	if !u.Enabled() {
		return nil, ErrUserDisabled
	}
	return u, nil
}

// GetUserAuthInfo 返回用户及其角色码、权限码，供签发 token 使用。
func (uc *UserUsecase) GetUserAuthInfo(ctx context.Context, userID int64) (*User, []string, error) {
	u, err := uc.repo.GetByID(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	codes, err := uc.repo.ListPermissionCodes(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	return u, codes, nil
}

// TouchLastLogin 记录登录时间，失败不影响登录本身。
func (uc *UserUsecase) TouchLastLogin(ctx context.Context, id int64) error {
	return uc.repo.TouchLastLogin(ctx, id)
}

// ListPermissionCodes 供鉴权中间件注入使用。
func (uc *UserUsecase) ListPermissionCodes(ctx context.Context, userID int64) ([]string, error) {
	return uc.repo.ListPermissionCodes(ctx, userID)
}

// dummyHash 用于在用户不存在时消耗与真实校验相当的 CPU 时间，
// 使两种失败路径的响应时长一致，消除账号枚举的计时侧信道。
//
// 启动时用随机口令真实算一遍，而不是硬编码一个哈希串：
// 手写的串一旦格式有误，Verify 会在解析阶段就返回，
// 根本跑不到 argon2 计算，等于这道防护静默失效。
var dummyHash = mustDummyHash()

func mustDummyHash() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		// 拿不到随机数就用固定串兜底，此时时序防护退化但不影响功能
		buf = []byte("eagle-fallback-dummy-password-32")
	}
	h, err := password.Hash(hex.EncodeToString(buf))
	if err != nil {
		return ""
	}
	return h
}
