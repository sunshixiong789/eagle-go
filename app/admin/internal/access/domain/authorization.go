package domain

import "context"

// AuthorizationChecker is the policy decision port owned by access.
type AuthorizationChecker interface {
	Allow(context.Context, []string, PermissionCode) (bool, error)
	Version(context.Context) (int64, error)
}
