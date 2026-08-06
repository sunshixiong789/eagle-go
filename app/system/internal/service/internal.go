package service

import (
	"context"
	"errors"

	v1 "github.com/eagle-go/eagle/api/eagle/system/v1"
	"github.com/eagle-go/eagle/app/system/internal/biz"
)

// InternalUserService 实现 v1.InternalUserService，只供 auth 服务经 gRPC 调用。
type InternalUserService struct {
	v1.UnimplementedInternalUserServiceServer

	uc *biz.UserUsecase
}

// NewInternalUserService 构造内部用户服务。
func NewInternalUserService(uc *biz.UserUsecase) *InternalUserService {
	return &InternalUserService{uc: uc}
}

// VerifyCredentials 校验账密。
//
// 账号不存在与口令错误都返回 ok=false 且不带任何区分信息，
// 由调用方统一回「用户名或密码错误」，避免账号枚举。
// 账号被停用是另一回事——可以如实告知，否则用户会一直以为自己记错了密码。
func (s *InternalUserService) VerifyCredentials(ctx context.Context, req *v1.VerifyCredentialsRequest) (*v1.VerifyCredentialsResponse, error) {
	u, err := s.uc.VerifyCredentials(ctx, req.GetUsername(), req.GetPassword())
	switch {
	case err == nil:
		return &v1.VerifyCredentialsResponse{
			Ok:       true,
			UserId:   u.ID,
			Username: u.Username,
			Nickname: u.Nickname,
			Status:   u.Status,
		}, nil

	case errors.Is(err, biz.ErrPasswordIncorrect):
		return &v1.VerifyCredentialsResponse{Ok: false}, nil

	case errors.Is(err, biz.ErrUserDisabled):
		// 账密正确但账号停用：ok=true + status=disabled，
		// 让调用方能给出准确提示而不是笼统的密码错误
		return &v1.VerifyCredentialsResponse{
			Ok:     true,
			Status: biz.StatusDisabled,
		}, nil

	default:
		// 数据库故障之类的真实错误照常抛出，不能伪装成认证失败
		return nil, err
	}
}

// GetUserAuthInfo 返回用户的角色码与权限码，供签发 token 使用。
func (s *InternalUserService) GetUserAuthInfo(ctx context.Context, req *v1.GetUserAuthInfoRequest) (*v1.GetUserAuthInfoResponse, error) {
	u, codes, err := s.uc.GetUserAuthInfo(ctx, req.GetUserId())
	if err != nil {
		return nil, err
	}
	return &v1.GetUserAuthInfoResponse{
		UserId:          u.ID,
		Username:        u.Username,
		Nickname:        u.Nickname,
		Email:           u.Email,
		Status:          u.Status,
		RoleCodes:       u.RoleCodes,
		PermissionCodes: codes,
	}, nil
}

// TouchLastLogin 记录登录时间。
func (s *InternalUserService) TouchLastLogin(ctx context.Context, req *v1.TouchLastLoginRequest) (*v1.TouchLastLoginResponse, error) {
	if err := s.uc.TouchLastLogin(ctx, req.GetUserId()); err != nil {
		return nil, err
	}
	return &v1.TouchLastLoginResponse{}, nil
}
