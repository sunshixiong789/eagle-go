package main

import (
	"log/slog"

	"github.com/go-kratos/kratos/v3"

	"github.com/eagle-go/eagle/app/system/internal/biz"
	"github.com/eagle-go/eagle/app/system/internal/conf"
	"github.com/eagle-go/eagle/app/system/internal/data"
	"github.com/eagle-go/eagle/app/system/internal/server"
	"github.com/eagle-go/eagle/app/system/internal/service"
)

// buildApp 显式装配 system 服务。依赖关系在这里一眼可见，新增组件时由
// 编译器直接检查签名，不需要额外的依赖注入生成步骤。
func buildApp(
	serverConf *conf.Server,
	dataConf *conf.Data,
	authConf *conf.Auth,
	logger *slog.Logger,
) (*kratos.App, func(), error) {
	d, closeData, err := data.NewData(dataConf)
	if err != nil {
		return nil, nil, err
	}
	fail := func(err error) (*kratos.App, func(), error) {
		closeData()
		return nil, nil, err
	}

	store := data.NewPolicyStore(data.NewEntClient(d))
	enforcer, err := data.NewEnforcer(store)
	if err != nil {
		return fail(err)
	}

	policyRepo := data.NewPolicyRepo(enforcer, store)
	permissionRepo := data.NewPermissionRepo(d)
	dictRepo := data.NewDictRepo(d)

	permissionUC := biz.NewPermissionUsecase(permissionRepo, policyRepo)
	dictUC := biz.NewDictUsecase(dictRepo)
	roleBindingUC := biz.NewRoleBindingUsecase(policyRepo, permissionRepo)

	permissionService := service.NewPermissionService(permissionUC)
	dictService := service.NewDictService(dictUC)
	roleBindingService := service.NewRoleBindingService(roleBindingUC)

	verifier := server.NewVerifier(authConf)
	middlewares, err := server.NewMiddlewares(logger, verifier, enforcer, authConf)
	if err != nil {
		return fail(err)
	}

	grpcServer := server.NewGRPCServer(
		serverConf, middlewares, permissionService, dictService, roleBindingService,
	)
	httpServer := server.NewHTTPServer(
		serverConf, middlewares, permissionService, dictService, roleBindingService,
	)

	unregisterPolicyHealth := data.RegisterPolicyHealth(store, enforcer)
	stopReconciler := data.NewPolicyReconciler(store, enforcer, logger)
	cleanup := func() {
		stopReconciler()
		unregisterPolicyHealth()
		closeData()
	}
	return newApp(logger, grpcServer, httpServer), cleanup, nil
}
