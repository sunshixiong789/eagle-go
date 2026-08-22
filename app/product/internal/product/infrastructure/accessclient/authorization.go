// Package access contains clients for the admin service's read-only access API.
package accessclient

import (
	"context"
	"fmt"

	"github.com/go-kratos/kratos/contrib/otel/v3/tracing"
	kratosgrpc "github.com/go-kratos/kratos/v3/transport/grpc"

	accessv1 "github.com/eagle-go/eagle/api/eagle/access/v1"
	"github.com/eagle-go/eagle/pkg/platform/config"
)

type Authorizer struct {
	client accessv1.AuthorizationServiceClient
}

func NewAuthorizer(c *config.Upstream) (*Authorizer, func(), error) {
	conn, err := kratosgrpc.NewClient(
		context.Background(),
		kratosgrpc.WithEndpoint(c.GetAuthorizationEndpoint()),
		kratosgrpc.WithTimeout(c.GetTimeout().AsDuration()),
		kratosgrpc.WithMiddleware(tracing.Client()),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("connect authorization service: %w", err)
	}
	cleanup := func() { _ = conn.Close() }
	return &Authorizer{client: accessv1.NewAuthorizationServiceClient(conn)}, cleanup, nil
}

func (a *Authorizer) AllowContext(ctx context.Context, roles []string, permission string) (bool, error) {
	resp, err := a.client.CheckPermission(ctx, &accessv1.CheckPermissionRequest{
		Roles: roles, Permission: permission,
	})
	if err != nil {
		return false, fmt.Errorf("check remote permission: %w", err)
	}
	return resp.GetAllowed(), nil
}

var _ interface {
	AllowContext(context.Context, []string, string) (bool, error)
} = (*Authorizer)(nil)
