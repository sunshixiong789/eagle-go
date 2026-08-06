package service

import (
	"context"

	"github.com/go-kratos/kratos/v3/errors"

	v1 "github.com/eagle-go/eagle/api/eagle/system/v1"
	"github.com/eagle-go/eagle/app/system/internal/biz"
	"github.com/eagle-go/eagle/pkg/identity"
)

// UserService 实现 v1.UserService。
type UserService struct {
	v1.UnimplementedUserServiceServer

	uc *biz.UserUsecase
}

// NewUserService 构造用户服务。
func NewUserService(uc *biz.UserUsecase) *UserService {
	return &UserService{uc: uc}
}

func toProtoUser(u *biz.User) *v1.User {
	if u == nil {
		return nil
	}
	return &v1.User{
		Id:          u.ID,
		Username:    u.Username,
		Nickname:    u.Nickname,
		Email:       u.Email,
		Phone:       u.Phone,
		DeptId:      u.DeptID,
		Status:      u.Status,
		Remark:      u.Remark,
		RoleIds:     u.RoleIDs,
		RoleCodes:   u.RoleCodes,
		LastLoginAt: tsPtr(u.LastLoginAt),
		CreatedAt:   ts(u.CreatedAt),
		UpdatedAt:   ts(u.UpdatedAt),
	}
}

// CreateUser 创建用户。
func (s *UserService) CreateUser(ctx context.Context, req *v1.CreateUserRequest) (*v1.CreateUserResponse, error) {
	u, err := s.uc.CreateUser(ctx, &biz.User{
		Username: req.GetUsername(),
		Nickname: req.GetNickname(),
		Email:    req.GetEmail(),
		Phone:    req.GetPhone(),
		DeptID:   req.GetDeptId(),
		Status:   req.GetStatus(),
		Remark:   req.GetRemark(),
	}, req.GetPassword(), req.GetRoleIds())
	if err != nil {
		return nil, err
	}
	return &v1.CreateUserResponse{User: toProtoUser(u)}, nil
}

// GetUser 查询单个用户。
func (s *UserService) GetUser(ctx context.Context, req *v1.GetUserRequest) (*v1.GetUserResponse, error) {
	u, err := s.uc.GetUser(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	return &v1.GetUserResponse{User: toProtoUser(u)}, nil
}

// ListUsers 分页查询用户。
func (s *UserService) ListUsers(ctx context.Context, req *v1.ListUsersRequest) (*v1.ListUsersResponse, error) {
	offset, limit := paginate(req.GetPage(), req.GetPageSize())

	users, total, err := s.uc.ListUsers(ctx, biz.ListUsersQuery{
		Keyword:  req.GetKeyword(),
		Status:   req.Status,
		DeptID:   req.DeptId,
		Offset:   offset,
		PageSize: limit,
	})
	if err != nil {
		return nil, err
	}

	out := make([]*v1.User, 0, len(users))
	for _, u := range users {
		out = append(out, toProtoUser(u))
	}
	return &v1.ListUsersResponse{Users: out, Total: total}, nil
}

// UpdateUser 更新用户资料。
func (s *UserService) UpdateUser(ctx context.Context, req *v1.UpdateUserRequest) (*v1.UpdateUserResponse, error) {
	u, err := s.uc.UpdateUser(ctx, &biz.User{
		ID:       req.GetId(),
		Nickname: req.GetNickname(),
		Email:    req.GetEmail(),
		Phone:    req.GetPhone(),
		DeptID:   req.GetDeptId(),
		Status:   req.GetStatus(),
		Remark:   req.GetRemark(),
	})
	if err != nil {
		return nil, err
	}
	return &v1.UpdateUserResponse{User: toProtoUser(u)}, nil
}

// DeleteUser 软删除用户。
func (s *UserService) DeleteUser(ctx context.Context, req *v1.DeleteUserRequest) (*v1.DeleteUserResponse, error) {
	if err := s.uc.DeleteUser(ctx, req.GetId()); err != nil {
		return nil, err
	}
	return &v1.DeleteUserResponse{}, nil
}

// ResetPassword 由管理员重置他人口令。
func (s *UserService) ResetPassword(ctx context.Context, req *v1.ResetPasswordRequest) (*v1.ResetPasswordResponse, error) {
	if err := s.uc.ResetPassword(ctx, req.GetId(), req.GetNewPassword()); err != nil {
		return nil, err
	}
	return &v1.ResetPasswordResponse{}, nil
}

// AssignRoles 全量覆盖用户的角色绑定。
func (s *UserService) AssignRoles(ctx context.Context, req *v1.AssignRolesRequest) (*v1.AssignRolesResponse, error) {
	if err := s.uc.AssignRoles(ctx, req.GetId(), req.GetRoleIds()); err != nil {
		return nil, err
	}
	return &v1.AssignRolesResponse{}, nil
}

// ChangeMyPassword 由当前登录用户修改自己的口令。
//
// 目标用户 ID 取自 token 而非请求体——如果让调用方传 ID，
// 任何登录用户都能改别人的密码，这类接口是越权的高发地带。
func (s *UserService) ChangeMyPassword(ctx context.Context, req *v1.ChangeMyPasswordRequest) (*v1.ChangeMyPasswordResponse, error) {
	uid := identity.UserID(ctx)
	if uid == 0 {
		return nil, errors.Unauthorized("UNAUTHENTICATED", "需要以用户身份登录")
	}

	if err := s.uc.ChangeOwnPassword(ctx, uid, req.GetOldPassword(), req.GetNewPassword()); err != nil {
		return nil, err
	}
	return &v1.ChangeMyPasswordResponse{}, nil
}
