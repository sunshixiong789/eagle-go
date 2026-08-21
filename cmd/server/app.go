package main

import (
	"log/slog"

	"github.com/go-kratos/kratos/v3"

	accessapp "github.com/eagle-go/eagle/internal/modules/access/application"
	accessinfra "github.com/eagle-go/eagle/internal/modules/access/infrastructure"
	accessinterfaces "github.com/eagle-go/eagle/internal/modules/access/interfaces"
	dictionaryapp "github.com/eagle-go/eagle/internal/modules/dictionary/application"
	dictionaryinfra "github.com/eagle-go/eagle/internal/modules/dictionary/infrastructure"
	dictionaryinterfaces "github.com/eagle-go/eagle/internal/modules/dictionary/interfaces"
	fileapp "github.com/eagle-go/eagle/internal/modules/file/application"
	fileinfra "github.com/eagle-go/eagle/internal/modules/file/infrastructure"
	fileinterfaces "github.com/eagle-go/eagle/internal/modules/file/interfaces"
	notificationapp "github.com/eagle-go/eagle/internal/modules/notification/application"
	notificationinfra "github.com/eagle-go/eagle/internal/modules/notification/infrastructure"
	notificationinterfaces "github.com/eagle-go/eagle/internal/modules/notification/interfaces"
	"github.com/eagle-go/eagle/internal/platform/config"
	platformdb "github.com/eagle-go/eagle/internal/platform/database"
	"github.com/eagle-go/eagle/internal/platform/server"
)

// buildApp 显式装配服务。依赖关系在这里一眼可见，新增组件时由
// 编译器直接检查签名，不需要额外的依赖注入生成步骤。
func buildApp(
	serverConf *config.Server,
	dataConf *config.Data,
	authConf *config.Auth,
	fileConf *config.File,
	logger *slog.Logger,
) (*kratos.App, func(), error) {
	db, closeDatabase, err := platformdb.Open(dataConf)
	if err != nil {
		return nil, nil, err
	}
	fail := func(err error) (*kratos.App, func(), error) {
		closeDatabase()
		return nil, nil, err
	}

	store := accessinfra.NewPolicyStore(db)
	enforcer, err := accessinfra.NewEnforcer(store)
	if err != nil {
		return fail(err)
	}

	policyRepo := accessinfra.NewPolicyRepo(enforcer, store)
	permissionRepo := accessinfra.NewPermissionRepo(db)
	dictRepo := dictionaryinfra.NewDictRepo(db)
	fileRepo := fileinfra.NewRepository(db)
	notificationRepo := notificationinfra.NewRepository(db)
	blobStore, err := fileinfra.NewLocalBlobStore(fileConf.GetLocalDir())
	if err != nil {
		return fail(err)
	}

	permissionUC := accessapp.NewPermissionUsecase(permissionRepo, policyRepo)
	dictUC := dictionaryapp.NewDictUsecase(dictRepo)
	roleBindingUC := accessapp.NewRoleBindingUsecase(policyRepo, permissionRepo)
	fileUC := fileapp.NewUsecase(fileRepo, blobStore, fileConf.GetMaxSizeBytes())
	notificationUC := notificationapp.NewUsecase(notificationRepo)

	permissionService := accessinterfaces.NewPermissionService(permissionUC)
	dictService := dictionaryinterfaces.NewDictService(dictUC)
	roleBindingService := accessinterfaces.NewRoleBindingService(roleBindingUC)
	fileService := fileinterfaces.NewFileService(fileUC)
	notificationService := notificationinterfaces.NewNotificationService(notificationUC)

	verifier := server.NewVerifier(authConf)
	middlewares, err := server.NewMiddlewares(logger, verifier, enforcer, authConf)
	if err != nil {
		return fail(err)
	}

	grpcServer := server.NewGRPCServer(
		serverConf, middlewares, permissionService, dictService, roleBindingService,
		fileService, notificationService,
	)
	httpServer := server.NewHTTPServer(
		serverConf, fileConf, middlewares, permissionService, dictService, roleBindingService,
		fileService, notificationService,
	)

	unregisterPolicyHealth := accessinfra.RegisterPolicyHealth(store, enforcer)
	stopReconciler := accessinfra.NewPolicyReconciler(store, enforcer, logger)
	cleanup := func() {
		stopReconciler()
		unregisterPolicyHealth()
		closeDatabase()
	}
	return newApp(logger, grpcServer, httpServer), cleanup, nil
}
