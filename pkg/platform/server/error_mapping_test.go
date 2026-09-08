package server

import (
	"context"
	"errors"
	"testing"

	kerrors "github.com/go-kratos/kratos/v3/errors"

	v1 "github.com/eagle-go/eagle/api/eagle/access/v1"
)

func TestToTransportErrorKeepsDistinctReasons(t *testing.T) {
	errInvalidPermissionCode := errors.New("invalid permission code")
	errButtonRequiresCode := errors.New("button requires code")
	errInvalidPermissionType := errors.New("invalid permission type")
	errEmptyPermissionName := errors.New("empty permission name")
	errEmptyRole := errors.New("empty role")
	errUnknownPermissionCode := errors.New("unknown permission code")
	errPermissionNotFound := errors.New("permission not found")
	rules := []ErrorMappingRule{
		BadRequest(errInvalidPermissionCode, v1.ErrorReason_ERROR_REASON_INVALID_PERMISSION_CODE),
		BadRequest(errButtonRequiresCode, v1.ErrorReason_ERROR_REASON_BUTTON_REQUIRES_CODE),
		BadRequest(errInvalidPermissionType, v1.ErrorReason_ERROR_REASON_INVALID_PERMISSION_TYPE),
		BadRequest(errEmptyPermissionName, v1.ErrorReason_ERROR_REASON_EMPTY_PERMISSION_NAME),
		BadRequest(errEmptyRole, v1.ErrorReason_ERROR_REASON_EMPTY_ROLE),
		BadRequest(errUnknownPermissionCode, v1.ErrorReason_ERROR_REASON_UNKNOWN_PERMISSION_CODE),
		NotFound(errPermissionNotFound, v1.ErrorReason_ERROR_REASON_PERMISSION_NOT_FOUND),
	}
	cases := []struct {
		err    error
		reason v1.ErrorReason
		code   int
	}{
		{errInvalidPermissionCode, v1.ErrorReason_ERROR_REASON_INVALID_PERMISSION_CODE, 400},
		{errButtonRequiresCode, v1.ErrorReason_ERROR_REASON_BUTTON_REQUIRES_CODE, 400},
		{errInvalidPermissionType, v1.ErrorReason_ERROR_REASON_INVALID_PERMISSION_TYPE, 400},
		{errEmptyPermissionName, v1.ErrorReason_ERROR_REASON_EMPTY_PERMISSION_NAME, 400},
		{errEmptyRole, v1.ErrorReason_ERROR_REASON_EMPTY_ROLE, 400},
		{errUnknownPermissionCode, v1.ErrorReason_ERROR_REASON_UNKNOWN_PERMISSION_CODE, 400},
		{errPermissionNotFound, v1.ErrorReason_ERROR_REASON_PERMISSION_NOT_FOUND, 404},
	}
	for _, tc := range cases {
		got := toTransportError(tc.err, rules)
		if kerrors.Code(got) != tc.code {
			t.Errorf("%v code = %d, want %d", tc.err, kerrors.Code(got), tc.code)
		}
		if kerrors.Reason(got) != tc.reason.String() {
			t.Errorf("%v reason = %s, want %s", tc.err, kerrors.Reason(got), tc.reason)
		}
	}
}

func TestErrorMappingMiddleware(t *testing.T) {
	domainErr := errors.New("conflict")
	wrapped := errors.Join(errors.New("context"), domainErr)
	rule := Conflict(domainErr, v1.ErrorReason_ERROR_REASON_PERMISSION_CODE_DUPLICATED)
	response, err := ErrorMapping(rule)(func(context.Context, any) (any, error) {
		return "partial", wrapped
	})(context.Background(), nil)
	if response != "partial" || kerrors.Code(err) != 409 ||
		kerrors.Reason(err) != v1.ErrorReason_ERROR_REASON_PERMISSION_CODE_DUPLICATED.String() || !errors.Is(err, wrapped) {
		t.Fatalf("response=%v, error=%v", response, err)
	}

	want := errors.New("unmapped")
	response, err = ErrorMapping(rule)(func(context.Context, any) (any, error) {
		return "unchanged", want
	})(context.Background(), nil)
	if response != "unchanged" || !errors.Is(err, want) {
		t.Fatalf("unmapped response=%v, error=%v", response, err)
	}
	if _, err := ErrorMapping(rule)(func(context.Context, any) (any, error) {
		return "ok", nil
	})(context.Background(), nil); err != nil {
		t.Fatalf("nil error changed to %v", err)
	}
}

func TestUnauthorizedMapping(t *testing.T) {
	domainErr := errors.New("invalid token")
	err := toTransportError(domainErr, []ErrorMappingRule{
		Unauthorized(domainErr, v1.ErrorReason_ERROR_REASON_UNSPECIFIED),
	})
	if kerrors.Code(err) != 401 {
		t.Fatalf("code = %d", kerrors.Code(err))
	}
}
