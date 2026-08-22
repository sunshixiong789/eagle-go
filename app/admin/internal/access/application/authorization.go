package application

import (
	"context"

	"github.com/eagle-go/eagle/app/admin/internal/access/domain"
)

// AuthorizationUsecase exposes read-only decisions to other services.
type AuthorizationUsecase struct{ checker domain.AuthorizationChecker }

func NewAuthorizationUsecase(checker domain.AuthorizationChecker) *AuthorizationUsecase {
	return &AuthorizationUsecase{checker: checker}
}

func (uc *AuthorizationUsecase) Check(ctx context.Context, roles []string, permission string) (bool, int64, error) {
	code, err := domain.NewPermissionCode(permission)
	if err != nil {
		return false, 0, err
	}
	allowed, err := uc.checker.Allow(ctx, roles, code)
	if err != nil {
		return false, 0, err
	}
	version, err := uc.checker.Version(ctx)
	if err != nil {
		return false, 0, err
	}
	return allowed, version, nil
}
