package infrastructure

import (
	"context"

	"github.com/eagle-go/eagle/app/admin/internal/access/domain"
	"github.com/eagle-go/eagle/pkg/authz"
)

type authorizationChecker struct {
	enforcer *authz.Enforcer
	store    *policyStore
}

func NewAuthorizationChecker(enforcer *authz.Enforcer, store *policyStore) domain.AuthorizationChecker {
	return &authorizationChecker{enforcer: enforcer, store: store}
}

func (c *authorizationChecker) Allow(ctx context.Context, roles []string, permission domain.PermissionCode) (bool, error) {
	return c.enforcer.AllowContext(ctx, roles, permission.String())
}

func (c *authorizationChecker) Version(ctx context.Context) (int64, error) {
	return c.store.PolicyVersion(ctx)
}
