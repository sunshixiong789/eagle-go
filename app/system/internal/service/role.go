package service

import (
	"context"

	v1 "github.com/eagle-go/eagle/api/eagle/system/v1"
	"github.com/eagle-go/eagle/app/system/internal/biz"
)

// RoleService 实现 v1.RoleService。
type RoleService struct {
	v1.UnimplementedRoleServiceServer

	uc *biz.RoleUsecase
}

// NewRoleService 构造角色服务。
func NewRoleService(uc *biz.RoleUsecase) *RoleService {
	return &RoleService{uc: uc}
}

func toProtoRole(r *biz.Role) *v1.Role {
	if r == nil {
		return nil
	}
	return &v1.Role{
		Id:            r.ID,
		Name:          r.Name,
		Code:          r.Code,
		Sort:          r.Sort,
		DataScope:     r.DataScope,
		Status:        r.Status,
		Remark:        r.Remark,
		PermissionIds: r.PermissionIDs,
		CreatedAt:     ts(r.CreatedAt),
		UpdatedAt:     ts(r.UpdatedAt),
	}
}

// CreateRole 创建角色。
func (s *RoleService) CreateRole(ctx context.Context, req *v1.CreateRoleRequest) (*v1.CreateRoleResponse, error) {
	r, err := s.uc.CreateRole(ctx, &biz.Role{
		Name:      req.GetName(),
		Code:      req.GetCode(),
		Sort:      req.GetSort(),
		DataScope: req.GetDataScope(),
		Status:    req.GetStatus(),
		Remark:    req.GetRemark(),
	}, req.GetPermissionIds())
	if err != nil {
		return nil, err
	}
	return &v1.CreateRoleResponse{Role: toProtoRole(r)}, nil
}

// GetRole 查询单个角色（含其权限 ID 列表）。
func (s *RoleService) GetRole(ctx context.Context, req *v1.GetRoleRequest) (*v1.GetRoleResponse, error) {
	r, err := s.uc.GetRole(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	return &v1.GetRoleResponse{Role: toProtoRole(r)}, nil
}

// ListRoles 分页查询角色。
func (s *RoleService) ListRoles(ctx context.Context, req *v1.ListRolesRequest) (*v1.ListRolesResponse, error) {
	offset, limit := paginate(req.GetPage(), req.GetPageSize())

	roles, total, err := s.uc.ListRoles(ctx, biz.ListRolesQuery{
		Keyword:  req.GetKeyword(),
		Status:   req.Status,
		Offset:   offset,
		PageSize: limit,
	})
	if err != nil {
		return nil, err
	}

	out := make([]*v1.Role, 0, len(roles))
	for _, r := range roles {
		out = append(out, toProtoRole(r))
	}
	return &v1.ListRolesResponse{Roles: out, Total: total}, nil
}

// UpdateRole 更新角色。角色码不在可更新字段之列。
func (s *RoleService) UpdateRole(ctx context.Context, req *v1.UpdateRoleRequest) (*v1.UpdateRoleResponse, error) {
	r, err := s.uc.UpdateRole(ctx, &biz.Role{
		ID:        req.GetId(),
		Name:      req.GetName(),
		Sort:      req.GetSort(),
		DataScope: req.GetDataScope(),
		Status:    req.GetStatus(),
		Remark:    req.GetRemark(),
	})
	if err != nil {
		return nil, err
	}
	return &v1.UpdateRoleResponse{Role: toProtoRole(r)}, nil
}

// DeleteRole 删除角色。
func (s *RoleService) DeleteRole(ctx context.Context, req *v1.DeleteRoleRequest) (*v1.DeleteRoleResponse, error) {
	if err := s.uc.DeleteRole(ctx, req.GetId()); err != nil {
		return nil, err
	}
	return &v1.DeleteRoleResponse{}, nil
}

// AssignPermissions 全量覆盖角色的权限。
func (s *RoleService) AssignPermissions(ctx context.Context, req *v1.AssignPermissionsRequest) (*v1.AssignPermissionsResponse, error) {
	if err := s.uc.AssignPermissions(ctx, req.GetId(), req.GetPermissionIds()); err != nil {
		return nil, err
	}
	return &v1.AssignPermissionsResponse{}, nil
}
