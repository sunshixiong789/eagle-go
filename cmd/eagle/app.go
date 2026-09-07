package main

import (
	accessv1 "github.com/eagle-go/eagle/api/eagle/access/v1"
	authv1 "github.com/eagle-go/eagle/api/eagle/auth/v1"
	dictionaryv1 "github.com/eagle-go/eagle/api/eagle/dictionary/v1"
	accessdomain "github.com/eagle-go/eagle/internal/access/domain"
	authdomain "github.com/eagle-go/eagle/internal/auth/domain"
	dictionarydomain "github.com/eagle-go/eagle/internal/dictionary/domain"
	"github.com/eagle-go/eagle/pkg/platform/server"
)

func errorMappings() []server.ErrorMappingRule {
	return []server.ErrorMappingRule{
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
