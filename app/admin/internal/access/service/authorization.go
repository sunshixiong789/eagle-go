package service

import (
	"context"

	accessv1 "github.com/eagle-go/eagle/api/eagle/access/v1"
	"github.com/eagle-go/eagle/app/admin/internal/access/application"
)

type AuthorizationService struct {
	accessv1.UnimplementedAuthorizationServiceServer
	uc *application.AuthorizationUsecase
}

func NewAuthorizationService(uc *application.AuthorizationUsecase) *AuthorizationService {
	return &AuthorizationService{uc: uc}
}

func (s *AuthorizationService) CheckPermission(ctx context.Context, req *accessv1.CheckPermissionRequest) (*accessv1.CheckPermissionResponse, error) {
	allowed, version, err := s.uc.Check(ctx, req.GetRoles(), req.GetPermission())
	if err != nil {
		return nil, err
	}
	return &accessv1.CheckPermissionResponse{Allowed: allowed, PolicyVersion: version}, nil
}
