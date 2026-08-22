package accessclient

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"

	accessv1 "github.com/eagle-go/eagle/api/eagle/access/v1"
)

type stubAuthorizationClient struct {
	response *accessv1.CheckPermissionResponse
	err      error
	request  *accessv1.CheckPermissionRequest
}

func (s *stubAuthorizationClient) CheckPermission(_ context.Context, request *accessv1.CheckPermissionRequest, _ ...grpc.CallOption) (*accessv1.CheckPermissionResponse, error) {
	s.request = request
	return s.response, s.err
}

func TestAuthorizerDelegatesDecisionAndFailsClosedOnError(t *testing.T) {
	stub := &stubAuthorizationClient{response: &accessv1.CheckPermissionResponse{Allowed: true}}
	authorizer := &Authorizer{client: stub}
	allowed, err := authorizer.AllowContext(context.Background(), []string{"realm:admin"}, "product:product:add")
	if err != nil || !allowed {
		t.Fatalf("AllowContext = %v, %v", allowed, err)
	}
	if stub.request.GetPermission() != "product:product:add" || len(stub.request.GetRoles()) != 1 {
		t.Fatalf("request = %+v", stub.request)
	}

	stub.err = errors.New("unavailable")
	if allowed, err := authorizer.AllowContext(context.Background(), nil, "product:product:add"); err == nil || allowed {
		t.Fatalf("failure result = %v, %v", allowed, err)
	}
}
