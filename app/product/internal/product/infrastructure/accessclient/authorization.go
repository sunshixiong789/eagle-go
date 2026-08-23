// Package access contains clients for the admin service's read-only access API.
package accessclient

import (
	"context"
	"fmt"
	"time"

	kratosgrpc "github.com/go-kratos/kratos/v3/transport/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	accessv1 "github.com/eagle-go/eagle/api/eagle/access/v1"
	"github.com/eagle-go/eagle/pkg/authn"
	platformclient "github.com/eagle-go/eagle/pkg/platform/client"
	"github.com/eagle-go/eagle/pkg/platform/config"
	"github.com/eagle-go/eagle/pkg/retryx"
)

type Authorizer struct {
	client      accessv1.AuthorizationServiceClient
	maxAttempts int
	backoff     time.Duration
}

func NewAuthorizer(c *config.Upstream, serviceAuth *config.ServiceAuth) (*Authorizer, func(), error) {
	credentials, err := authn.NewClientCredentials(authn.ClientCredentialsConfig{
		TokenURL: serviceAuth.GetTokenUrl(), ClientID: serviceAuth.GetClientId(), ClientSecret: serviceAuth.GetClientSecret(),
	})
	if err != nil {
		return nil, nil, err
	}
	middlewares, err := platformclient.NewMiddlewares(credentials.Client())
	if err != nil {
		return nil, nil, err
	}
	conn, err := kratosgrpc.NewClient(
		context.Background(),
		kratosgrpc.WithEndpoint(c.GetAuthorizationEndpoint()),
		kratosgrpc.WithTimeout(c.GetTimeout().AsDuration()),
		kratosgrpc.WithMiddleware(middlewares...),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("connect authorization service: %w", err)
	}
	cleanup := func() { _ = conn.Close() }
	return &Authorizer{
		client: accessv1.NewAuthorizationServiceClient(conn), maxAttempts: int(c.GetMaxAttempts()),
		backoff: c.GetRetryBackoff().AsDuration(),
	}, cleanup, nil
}

func (a *Authorizer) AllowContext(ctx context.Context, roles []string, permission string) (bool, error) {
	var resp *accessv1.CheckPermissionResponse
	err := retryx.Do(ctx, a.maxAttempts, a.backoff, retryable, func() error {
		var err error
		resp, err = a.client.CheckPermission(ctx, &accessv1.CheckPermissionRequest{Roles: roles, Permission: permission})
		return err
	})
	if err != nil {
		return false, fmt.Errorf("check remote permission: %w", err)
	}
	return resp.GetAllowed(), nil
}

func retryable(err error) bool {
	code := status.Code(err)
	return code == codes.Unavailable || code == codes.ResourceExhausted
}

var _ interface {
	AllowContext(context.Context, []string, string) (bool, error)
} = (*Authorizer)(nil)
