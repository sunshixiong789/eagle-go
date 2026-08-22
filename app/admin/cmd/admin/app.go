package main

import (
	"log/slog"

	"github.com/go-kratos/kratos/v3/transport"
	"github.com/go-kratos/kratos/v3/transport/grpc"
	"github.com/go-kratos/kratos/v3/transport/http"

	accessv1 "github.com/eagle-go/eagle/api/eagle/access/v1"
	dictionaryv1 "github.com/eagle-go/eagle/api/eagle/dictionary/v1"
	filev1 "github.com/eagle-go/eagle/api/eagle/file/v1"
	notificationv1 "github.com/eagle-go/eagle/api/eagle/notification/v1"
	accessapp "github.com/eagle-go/eagle/app/admin/internal/access/application"
	accessdomain "github.com/eagle-go/eagle/app/admin/internal/access/domain"
	accessinfra "github.com/eagle-go/eagle/app/admin/internal/access/infrastructure"
	accessservice "github.com/eagle-go/eagle/app/admin/internal/access/service"
	dictionaryapp "github.com/eagle-go/eagle/app/admin/internal/dictionary/application"
	dictionarydomain "github.com/eagle-go/eagle/app/admin/internal/dictionary/domain"
	dictionaryinfra "github.com/eagle-go/eagle/app/admin/internal/dictionary/infrastructure"
	dictionaryservice "github.com/eagle-go/eagle/app/admin/internal/dictionary/service"
	fileapp "github.com/eagle-go/eagle/app/admin/internal/file/application"
	filedomain "github.com/eagle-go/eagle/app/admin/internal/file/domain"
	fileinfra "github.com/eagle-go/eagle/app/admin/internal/file/infrastructure"
	fileservice "github.com/eagle-go/eagle/app/admin/internal/file/service"
	notificationapp "github.com/eagle-go/eagle/app/admin/internal/notification/application"
	notificationdomain "github.com/eagle-go/eagle/app/admin/internal/notification/domain"
	notificationinfra "github.com/eagle-go/eagle/app/admin/internal/notification/infrastructure"
	notificationservice "github.com/eagle-go/eagle/app/admin/internal/notification/service"
	platformdb "github.com/eagle-go/eagle/app/admin/internal/platform/database"
	"github.com/eagle-go/eagle/pkg/platform/config"
	platformruntime "github.com/eagle-go/eagle/pkg/platform/runtime"
	"github.com/eagle-go/eagle/pkg/platform/server"
)

func buildApp(bc *config.Bootstrap, logger *slog.Logger) (platformruntime.Components, error) {
	db, closeDB, err := platformdb.Open(bc.GetData())
	if err != nil {
		return platformruntime.Components{}, err
	}
	fail := func(err error) (platformruntime.Components, error) {
		closeDB()
		return platformruntime.Components{}, err
	}

	store := accessinfra.NewPolicyStore(db)
	enforcer, err := accessinfra.NewEnforcer(store)
	if err != nil {
		return fail(err)
	}
	permissionRepo := accessinfra.NewPermissionRepo(db)
	policyRepo := accessinfra.NewPolicyRepo(enforcer, store)
	permissionService := accessservice.NewPermissionService(accessapp.NewPermissionUsecase(permissionRepo, policyRepo))
	roleService := accessservice.NewRoleBindingService(accessapp.NewRoleBindingUsecase(policyRepo, permissionRepo))
	dictService := dictionaryservice.NewDictService(dictionaryapp.NewDictUsecase(dictionaryinfra.NewDictRepo(db)))
	blobs, err := fileinfra.NewLocalBlobStore(bc.GetFile().GetLocalDir())
	if err != nil {
		return fail(err)
	}
	fileService := fileservice.NewFileService(fileapp.NewUsecase(
		fileinfra.NewRepository(db), blobs, bc.GetFile().GetMaxSizeBytes(),
	))
	notificationService := notificationservice.NewNotificationService(
		notificationapp.NewUsecase(notificationinfra.NewRepository(db)),
	)
	authorizationService := accessservice.NewAuthorizationService(accessapp.NewAuthorizationUsecase(
		accessinfra.NewAuthorizationChecker(enforcer, store),
	))

	ms, err := server.NewMiddlewares(logger, server.NewVerifier(bc.GetAuth()), enforcer, bc.GetAuth(), adminErrorMappings()...)
	if err != nil {
		return fail(err)
	}
	gs := server.NewGRPCServer(bc.GetServer(), ms, func(s *grpc.Server) {
		accessv1.RegisterPermissionServiceServer(s, permissionService)
		accessv1.RegisterRoleBindingServiceServer(s, roleService)
		accessv1.RegisterAuthorizationServiceServer(s, authorizationService)
		dictionaryv1.RegisterDictServiceServer(s, dictService)
		filev1.RegisterFileServiceServer(s, fileService)
		notificationv1.RegisterNotificationServiceServer(s, notificationService)
	})
	hs := server.NewHTTPServer(bc.GetServer(), ms, []http.FilterFunc{
		server.FileUploadLimitFilter(bc.GetFile().GetMaxSizeBytes()),
	}, func(s *http.Server) {
		accessv1.RegisterPermissionServiceHTTPServer(s, permissionService)
		accessv1.RegisterRoleBindingServiceHTTPServer(s, roleService)
		dictionaryv1.RegisterDictServiceHTTPServer(s, dictService)
		filev1.RegisterFileServiceHTTPServer(s, fileService)
		notificationv1.RegisterNotificationServiceHTTPServer(s, notificationService)
	})
	unregisterHealth := accessinfra.RegisterPolicyHealth(store, enforcer)
	stopReconciler := accessinfra.NewPolicyReconciler(store, enforcer, logger)
	return platformruntime.Components{
		Servers: []transport.Server{gs, hs},
		Cleanup: func() { stopReconciler(); unregisterHealth(); closeDB() },
	}, nil
}

func adminErrorMappings() []server.ErrorMappingRule {
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
		server.BadRequest(accessdomain.ErrUnknownPermissionCode, accessv1.ErrorReason_ERROR_REASON_UNKNOWN_PERMISSION_CODE),
		server.BadRequest(accessdomain.ErrEmptyRole, accessv1.ErrorReason_ERROR_REASON_EMPTY_ROLE),
		server.BadRequest(accessdomain.ErrSelfInheritance, accessv1.ErrorReason_ERROR_REASON_ROLE_INHERITANCE_CYCLE),
		server.BadRequest(accessdomain.ErrRoleInheritanceCycle, accessv1.ErrorReason_ERROR_REASON_ROLE_INHERITANCE_CYCLE),
		server.NotFound(dictionarydomain.ErrDictTypeNotFound, dictionaryv1.ErrorReason_ERROR_REASON_DICT_TYPE_NOT_FOUND),
		server.Conflict(dictionarydomain.ErrDictTypeDuplicated, dictionaryv1.ErrorReason_ERROR_REASON_DICT_TYPE_DUPLICATED),
		server.NotFound(dictionarydomain.ErrDictDataNotFound, dictionaryv1.ErrorReason_ERROR_REASON_DICT_DATA_NOT_FOUND),
		server.Conflict(dictionarydomain.ErrDictDataDuplicated, dictionaryv1.ErrorReason_ERROR_REASON_DICT_DATA_DUPLICATED),
		server.NotFound(filedomain.ErrFileNotFound, filev1.ErrorReason_ERROR_REASON_FILE_NOT_FOUND),
		server.BadRequest(filedomain.ErrFileTooLarge, filev1.ErrorReason_ERROR_REASON_FILE_TOO_LARGE),
		server.BadRequest(filedomain.ErrInvalidFileName, filev1.ErrorReason_ERROR_REASON_INVALID_FILE_NAME),
		server.BadRequest(filedomain.ErrInvalidContentType, filev1.ErrorReason_ERROR_REASON_INVALID_FILE_CONTENT_TYPE),
		server.NotFound(notificationdomain.ErrNotificationNotFound, notificationv1.ErrorReason_ERROR_REASON_NOTIFICATION_NOT_FOUND),
	}
}
