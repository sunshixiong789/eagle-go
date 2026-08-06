package service

import (
	"context"

	"github.com/go-kratos/kratos/v3/errors"

	v1 "github.com/eagle-go/eagle/api/eagle/system/v1"
	"github.com/eagle-go/eagle/app/system/internal/biz"
	"github.com/eagle-go/eagle/pkg/identity"
)

// RoleBindingService 实现 v1.RoleBindingService。
type RoleBindingService struct {
	v1.UnimplementedRoleBindingServiceServer

	uc *biz.RoleBindingUsecase
}

// NewRoleBindingService 构造角色绑定服务。
func NewRoleBindingService(uc *biz.RoleBindingUsecase) *RoleBindingService {
	return &RoleBindingService{uc: uc}
}

func toProtoBinding(b *biz.RoleBinding) *v1.RoleBinding {
	if b == nil {
		return nil
	}
	return &v1.RoleBinding{
		Role:            b.Role,
		PermissionCodes: b.PermissionCodes,
	}
}

// ListBoundRoles 列出已配置权限的角色。
func (s *RoleBindingService) ListBoundRoles(ctx context.Context, _ *v1.ListBoundRolesRequest) (*v1.ListBoundRolesResponse, error) {
	bindings, err := s.uc.ListBoundRoles(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]*v1.RoleBinding, 0, len(bindings))
	for _, b := range bindings {
		out = append(out, toProtoBinding(b))
	}
	return &v1.ListBoundRolesResponse{Bindings: out}, nil
}

// GetRolePermissions 返回单个角色的权限码。
func (s *RoleBindingService) GetRolePermissions(ctx context.Context, req *v1.GetRolePermissionsRequest) (*v1.GetRolePermissionsResponse, error) {
	b, err := s.uc.GetRolePermissions(ctx, req.GetRole())
	if err != nil {
		return nil, err
	}
	return &v1.GetRolePermissionsResponse{Binding: toProtoBinding(b)}, nil
}

// SetRolePermissions 全量覆盖角色的权限码。
func (s *RoleBindingService) SetRolePermissions(ctx context.Context, req *v1.SetRolePermissionsRequest) (*v1.SetRolePermissionsResponse, error) {
	if err := s.uc.SetRolePermissions(ctx, req.GetRole(), req.GetPermissionCodes()); err != nil {
		return nil, err
	}
	return &v1.SetRolePermissionsResponse{}, nil
}

// AddRoleInheritance 建立角色继承。
func (s *RoleBindingService) AddRoleInheritance(ctx context.Context, req *v1.AddRoleInheritanceRequest) (*v1.AddRoleInheritanceResponse, error) {
	if err := s.uc.AddRoleInheritance(ctx, req.GetChild(), req.GetParent()); err != nil {
		return nil, err
	}
	return &v1.AddRoleInheritanceResponse{}, nil
}

// GetMyPermissions 返回当前登录者的角色与展开后的权限码。
//
// 角色取自 token，前端拿这份结果做按钮级显隐。
func (s *RoleBindingService) GetMyPermissions(ctx context.Context, _ *v1.GetMyPermissionsRequest) (*v1.GetMyPermissionsResponse, error) {
	p, ok := identity.FromContext(ctx)
	if !ok {
		return nil, errors.Unauthorized("UNAUTHENTICATED", "需要登录")
	}

	codes, err := s.uc.PermissionsOf(ctx, p.Roles)
	if err != nil {
		return nil, err
	}
	return &v1.GetMyPermissionsResponse{
		Roles:           p.Roles,
		PermissionCodes: codes,
	}, nil
}
