package server

import (
	"testing"

	kerrors "github.com/go-kratos/kratos/v3/errors"

	v1 "github.com/eagle-go/eagle/api/eagle/access/v1"
	domain "github.com/eagle-go/eagle/internal/modules/access/domain"
)

func TestToTransportErrorKeepsDistinctReasons(t *testing.T) {
	cases := []struct {
		err    error
		reason v1.ErrorReason
		code   int
	}{
		{domain.ErrInvalidPermissionCode, v1.ErrorReason_ERROR_REASON_INVALID_PERMISSION_CODE, 400},
		{domain.ErrButtonRequiresCode, v1.ErrorReason_ERROR_REASON_BUTTON_REQUIRES_CODE, 400},
		{domain.ErrInvalidPermissionType, v1.ErrorReason_ERROR_REASON_INVALID_PERMISSION_TYPE, 400},
		{domain.ErrEmptyPermissionName, v1.ErrorReason_ERROR_REASON_EMPTY_PERMISSION_NAME, 400},
		{domain.ErrEmptyRole, v1.ErrorReason_ERROR_REASON_EMPTY_ROLE, 400},
		{domain.ErrUnknownPermissionCode, v1.ErrorReason_ERROR_REASON_UNKNOWN_PERMISSION_CODE, 400},
		{domain.ErrPermissionNotFound, v1.ErrorReason_ERROR_REASON_PERMISSION_NOT_FOUND, 404},
	}
	for _, tc := range cases {
		got := toTransportError(tc.err)
		if kerrors.Code(got) != tc.code {
			t.Errorf("%v code = %d, want %d", tc.err, kerrors.Code(got), tc.code)
		}
		if kerrors.Reason(got) != tc.reason.String() {
			t.Errorf("%v reason = %s, want %s", tc.err, kerrors.Reason(got), tc.reason)
		}
	}
}
