package data

import (
	"context"
	"fmt"

	"github.com/eagle-go/eagle/app/system/internal/biz"
	"github.com/eagle-go/eagle/pkg/db/sqlc"
)

type userRepo struct {
	data *Data
}

// NewUserRepo 构造用户仓储。
func NewUserRepo(data *Data) biz.UserRepo {
	return &userRepo{data: data}
}

func toBizUser(u sqlc.SysUser) *biz.User {
	return &biz.User{
		ID:          u.ID,
		Username:    u.Username,
		Nickname:    u.Nickname,
		Email:       u.Email,
		Phone:       u.Phone,
		DeptID:      u.DeptID,
		Status:      toInt32(u.Status),
		Remark:      u.Remark,
		LastLoginAt: u.LastLoginAt,
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
	}
}

// Create 在一个事务里写入用户并绑定角色，避免出现「用户建好了但没有角色」的中间态。
func (r *userRepo) Create(ctx context.Context, u *biz.User, passwordHash string, roleIDs []int64) (*biz.User, error) {
	var created sqlc.SysUser

	err := r.data.db.Tx(ctx, func(q *sqlc.Queries) error {
		var err error
		created, err = q.CreateUser(ctx, sqlc.CreateUserParams{
			Username:     u.Username,
			PasswordHash: passwordHash,
			Nickname:     u.Nickname,
			Email:        u.Email,
			Phone:        u.Phone,
			DeptID:       u.DeptID,
			Status:       toInt16(u.Status),
			Remark:       u.Remark,
		})
		if err != nil {
			return err
		}
		for _, roleID := range roleIDs {
			if err := q.AddUserRole(ctx, sqlc.AddUserRoleParams{
				UserID: created.ID,
				RoleID: roleID,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, biz.ErrUserAlreadyExists
		}
		if isForeignKeyViolation(err) {
			return nil, biz.ErrRoleNotFound
		}
		return nil, fmt.Errorf("create user: %w", err)
	}

	out := toBizUser(created)
	out.RoleIDs = roleIDs
	return out, nil
}

func (r *userRepo) GetByID(ctx context.Context, id int64) (*biz.User, error) {
	u, err := r.data.db.GetUserByID(ctx, id)
	if err != nil {
		if isNoRows(err) {
			return nil, biz.ErrUserNotFound
		}
		return nil, fmt.Errorf("get user %d: %w", id, err)
	}

	out := toBizUser(u)
	if err := r.fillRoles(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *userRepo) GetByUsernameWithSecret(ctx context.Context, username string) (*biz.User, string, error) {
	u, err := r.data.db.GetUserByUsername(ctx, username)
	if err != nil {
		if isNoRows(err) {
			return nil, "", biz.ErrUserNotFound
		}
		return nil, "", fmt.Errorf("get user by username: %w", err)
	}

	out := toBizUser(u)
	if err := r.fillRoles(ctx, out); err != nil {
		return nil, "", err
	}
	return out, u.PasswordHash, nil
}

// fillRoles 补齐用户的角色 ID 与角色码。
// 角色码要进 token 并参与超管短路判定，所以取用户时一并加载。
func (r *userRepo) fillRoles(ctx context.Context, u *biz.User) error {
	roles, err := r.data.db.ListRolesByUserID(ctx, u.ID)
	if err != nil {
		return fmt.Errorf("list roles of user %d: %w", u.ID, err)
	}
	u.RoleIDs = make([]int64, 0, len(roles))
	u.RoleCodes = make([]string, 0, len(roles))
	for _, role := range roles {
		u.RoleIDs = append(u.RoleIDs, role.ID)
		u.RoleCodes = append(u.RoleCodes, role.Code)
	}
	return nil
}

func (r *userRepo) List(ctx context.Context, q biz.ListUsersQuery) ([]*biz.User, int64, error) {
	rows, err := r.data.db.ListUsers(ctx, sqlc.ListUsersParams{
		Keyword:    nilIfEmpty(q.Keyword),
		Status:     int32PtrToInt16Ptr(q.Status),
		DeptID:     q.DeptID,
		PageOffset: q.Offset,
		PageSize:   q.PageSize,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}

	total, err := r.data.db.CountUsers(ctx, sqlc.CountUsersParams{
		Keyword: nilIfEmpty(q.Keyword),
		Status:  int32PtrToInt16Ptr(q.Status),
		DeptID:  q.DeptID,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}

	users := make([]*biz.User, 0, len(rows))
	for _, row := range rows {
		users = append(users, toBizUser(row))
	}
	return users, total, nil
}

func (r *userRepo) Update(ctx context.Context, u *biz.User) (*biz.User, error) {
	updated, err := r.data.db.UpdateUser(ctx, sqlc.UpdateUserParams{
		ID:       u.ID,
		Nickname: u.Nickname,
		Email:    u.Email,
		Phone:    u.Phone,
		DeptID:   u.DeptID,
		Status:   toInt16(u.Status),
		Remark:   u.Remark,
	})
	if err != nil {
		if isNoRows(err) {
			return nil, biz.ErrUserNotFound
		}
		return nil, fmt.Errorf("update user %d: %w", u.ID, err)
	}

	out := toBizUser(updated)
	if err := r.fillRoles(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *userRepo) UpdatePassword(ctx context.Context, id int64, passwordHash string) error {
	rows, err := r.data.db.UpdateUserPassword(ctx, sqlc.UpdateUserPasswordParams{
		ID:           id,
		PasswordHash: passwordHash,
	})
	if err != nil {
		return fmt.Errorf("update password of user %d: %w", id, err)
	}
	if rows == 0 {
		return biz.ErrUserNotFound
	}
	return nil
}

func (r *userRepo) GetPasswordHash(ctx context.Context, id int64) (string, error) {
	u, err := r.data.db.GetUserByID(ctx, id)
	if err != nil {
		if isNoRows(err) {
			return "", biz.ErrUserNotFound
		}
		return "", fmt.Errorf("get password hash of user %d: %w", id, err)
	}
	return u.PasswordHash, nil
}

func (r *userRepo) Delete(ctx context.Context, id int64) error {
	rows, err := r.data.db.SoftDeleteUser(ctx, id)
	if err != nil {
		return fmt.Errorf("delete user %d: %w", id, err)
	}
	if rows == 0 {
		return biz.ErrUserNotFound
	}
	return nil
}

// AssignRoles 全量覆盖用户的角色绑定。
// 先删后插放在同一事务里，避免中途失败导致用户短暂没有任何角色。
func (r *userRepo) AssignRoles(ctx context.Context, userID int64, roleIDs []int64) error {
	err := r.data.db.Tx(ctx, func(q *sqlc.Queries) error {
		if err := q.DeleteUserRoles(ctx, userID); err != nil {
			return err
		}
		for _, roleID := range roleIDs {
			if err := q.AddUserRole(ctx, sqlc.AddUserRoleParams{
				UserID: userID,
				RoleID: roleID,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		if isForeignKeyViolation(err) {
			return biz.ErrRoleNotFound
		}
		return fmt.Errorf("assign roles to user %d: %w", userID, err)
	}
	return nil
}

func (r *userRepo) TouchLastLogin(ctx context.Context, id int64) error {
	if err := r.data.db.TouchUserLastLogin(ctx, id); err != nil {
		return fmt.Errorf("touch last login of user %d: %w", id, err)
	}
	return nil
}

// ListPermissionCodes 是鉴权中间件的热路径，走 Redis 缓存。
func (r *userRepo) ListPermissionCodes(ctx context.Context, userID int64) ([]string, error) {
	return cached(ctx, r.data, userPermKey(userID), r.data.cache.permTTL,
		func(ctx context.Context) ([]string, error) {
			codes, err := r.data.db.ListPermissionCodesByUserID(ctx, userID)
			if err != nil {
				return nil, fmt.Errorf("list permission codes of user %d: %w", userID, err)
			}
			return codes, nil
		})
}

func (r *userRepo) InvalidatePermissionCache(ctx context.Context, userIDs ...int64) error {
	keys := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		keys = append(keys, userPermKey(id))
	}
	return r.data.invalidate(ctx, keys...)
}
