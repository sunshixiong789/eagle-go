package main

import (
	"context"
	"errors"

	accessv1 "github.com/eagle-go/eagle/api/eagle/access/v1"
	authv1 "github.com/eagle-go/eagle/api/eagle/auth/v1"
	dictionaryv1 "github.com/eagle-go/eagle/api/eagle/dictionary/v1"
	accessdomain "github.com/eagle-go/eagle/internal/access/domain"
	authdomain "github.com/eagle-go/eagle/internal/auth/domain"
	dictionarydomain "github.com/eagle-go/eagle/internal/dictionary/domain"
	"github.com/eagle-go/eagle/internal/platform/server"
	"github.com/eagle-go/eagle/pkg/authn"
)

// sessionActivity 把会话存储结果翻译成认证中间件识别的哨兵错误。
func sessionActivity(sessions authdomain.SessionManager) func(context.Context, string, string) error {
	return func(ctx context.Context, subject, sessionID string) error {
		err := sessions.AccessActive(ctx, subject, sessionID)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, authdomain.ErrAccountDisabled):
			return authn.ErrAccountDisabled
		case errors.Is(err, authdomain.ErrSessionInactive):
			return authn.ErrSessionInactive
		default:
			return err
		}
	}
}

func errorMappings() []server.ErrorMappingRule {
	return []server.ErrorMappingRule{
		server.BadRequest(accessdomain.ErrInvalidPermissionStatus, accessv1.ErrorReason_ERROR_REASON_INVALID_PERMISSION_STATUS),
		server.NotFound(accessdomain.ErrPermissionNotFound, accessv1.ErrorReason_ERROR_REASON_PERMISSION_NOT_FOUND),
		server.Conflict(accessdomain.ErrPermissionCodeDuplicated, accessv1.ErrorReason_ERROR_REASON_PERMISSION_CODE_DUPLICATED),
		server.Conflict(accessdomain.ErrPermissionHasChildren, accessv1.ErrorReason_ERROR_REASON_PERMISSION_HAS_CHILDREN),
		server.BadRequest(accessdomain.ErrPermissionCycle, accessv1.ErrorReason_ERROR_REASON_PERMISSION_CYCLE),
		server.Conflict(accessdomain.ErrConcurrentModification, accessv1.ErrorReason_ERROR_REASON_CONCURRENT_MODIFICATION),
		server.BadRequest(accessdomain.ErrInvalidPermissionCode, accessv1.ErrorReason_ERROR_REASON_INVALID_PERMISSION_CODE),
		server.BadRequest(accessdomain.ErrButtonRequiresCode, accessv1.ErrorReason_ERROR_REASON_BUTTON_REQUIRES_CODE),
		server.BadRequest(accessdomain.ErrInvalidPermissionType, accessv1.ErrorReason_ERROR_REASON_INVALID_PERMISSION_TYPE),
		server.BadRequest(accessdomain.ErrEmptyPermissionName, accessv1.ErrorReason_ERROR_REASON_EMPTY_PERMISSION_NAME),
		server.NotFound(accessdomain.ErrRoleNotBound, accessv1.ErrorReason_ERROR_REASON_ROLE_NOT_BOUND),
		server.BadRequest(authdomain.ErrProviderDisabled, authv1.ErrorReason_ERROR_REASON_PROVIDER_DISABLED),
		server.Unauthorized(authdomain.ErrInvalidIDToken, authv1.ErrorReason_ERROR_REASON_INVALID_ID_TOKEN),
		server.Unauthorized(authdomain.ErrInvalidNonce, authv1.ErrorReason_ERROR_REASON_INVALID_NONCE),
		server.Unauthorized(authdomain.ErrInvalidRefreshToken, authv1.ErrorReason_ERROR_REASON_INVALID_REFRESH_TOKEN),
		server.Unauthorized(authdomain.ErrCredentialUsed, authv1.ErrorReason_ERROR_REASON_CREDENTIAL_USED),
		server.Unauthorized(authdomain.ErrSessionInactive, authv1.ErrorReason_ERROR_REASON_INVALID_REFRESH_TOKEN),
		server.Forbidden(authdomain.ErrAccountDisabled, authv1.ErrorReason_ERROR_REASON_ACCOUNT_DISABLED),
		server.NotFound(authdomain.ErrSessionNotFound, authv1.ErrorReason_ERROR_REASON_SESSION_NOT_FOUND),
		server.BadRequest(authdomain.ErrCannotRevokeCurrent, authv1.ErrorReason_ERROR_REASON_CANNOT_REVOKE_CURRENT),
		server.NotFound(authdomain.ErrAccountNotFound, authv1.ErrorReason_ERROR_REASON_ACCOUNT_NOT_FOUND),
		server.BadRequest(authdomain.ErrInvalidRoleAssignment, authv1.ErrorReason_ERROR_REASON_INVALID_ROLE_ASSIGNMENT),
		server.Conflict(authdomain.ErrRoleRevisionConflict, authv1.ErrorReason_ERROR_REASON_ROLE_REVISION_CONFLICT),
		server.Conflict(authdomain.ErrLastAdmin, authv1.ErrorReason_ERROR_REASON_LAST_ADMIN),
		server.BadRequest(accessdomain.ErrUnknownPermissionCode, accessv1.ErrorReason_ERROR_REASON_UNKNOWN_PERMISSION_CODE),
		server.BadRequest(accessdomain.ErrEmptyRole, accessv1.ErrorReason_ERROR_REASON_EMPTY_ROLE),
		server.BadRequest(accessdomain.ErrSelfInheritance, accessv1.ErrorReason_ERROR_REASON_ROLE_INHERITANCE_CYCLE),
		server.BadRequest(accessdomain.ErrRoleInheritanceCycle, accessv1.ErrorReason_ERROR_REASON_ROLE_INHERITANCE_CYCLE),
		server.NotFound(dictionarydomain.ErrDictTypeNotFound, dictionaryv1.ErrorReason_ERROR_REASON_DICT_TYPE_NOT_FOUND),
		server.Conflict(dictionarydomain.ErrDictTypeDuplicated, dictionaryv1.ErrorReason_ERROR_REASON_DICT_TYPE_DUPLICATED),
		server.NotFound(dictionarydomain.ErrDictDataNotFound, dictionaryv1.ErrorReason_ERROR_REASON_DICT_DATA_NOT_FOUND),
		server.Conflict(dictionarydomain.ErrDictDataDuplicated, dictionaryv1.ErrorReason_ERROR_REASON_DICT_DATA_DUPLICATED),
	}
}
