package main

import (
	"log/slog"

	"github.com/go-kratos/kratos/v3/transport"
	"github.com/go-kratos/kratos/v3/transport/http"

	accessv1 "github.com/eagle-go/eagle/api/eagle/access/v1"
	authv1 "github.com/eagle-go/eagle/api/eagle/auth/v1"
	dictionaryv1 "github.com/eagle-go/eagle/api/eagle/dictionary/v1"
	accessapp "github.com/eagle-go/eagle/internal/access/application"
	accessinfra "github.com/eagle-go/eagle/internal/access/infrastructure"
	accessinterfaces "github.com/eagle-go/eagle/internal/access/interfaces"
	authapp "github.com/eagle-go/eagle/internal/auth/application"
	authinfra "github.com/eagle-go/eagle/internal/auth/infrastructure"
	authinterfaces "github.com/eagle-go/eagle/internal/auth/interfaces"
	dictionaryinfra "github.com/eagle-go/eagle/internal/dictionary/infrastructure"
	dictionaryinterfaces "github.com/eagle-go/eagle/internal/dictionary/interfaces"
	platformdb "github.com/eagle-go/eagle/internal/platform/database"
	"github.com/eagle-go/eagle/pkg/platform/config"
	platformruntime "github.com/eagle-go/eagle/pkg/platform/runtime"
	"github.com/eagle-go/eagle/pkg/platform/server"
)

// composeApp 是唯一组合根。所有可能失败的构造完成后才启动后台任务。
func composeApp(bc *config.Bootstrap, logger *slog.Logger) (platformruntime.Components, error) {
	db, closeDB, err := platformdb.Open(bc.GetData())
	if err != nil {
		return platformruntime.Components{}, err
	}
	ready := false
	defer func() {
		if !ready {
			closeDB()
		}
	}()
	store := accessinfra.NewPolicyStore(db)
	enforcer, err := accessinfra.NewEnforcer(store)
	if err != nil {
		return platformruntime.Components{}, err
	}
	auth := bc.GetAuth()
	ms, err := server.NewMiddlewares(logger, server.NewVerifier(auth), enforcer, errorMappings()...)
	if err != nil {
		return platformruntime.Components{}, err
	}
	issuer, err := authinfra.NewTokenIssuer(auth.GetSigningSecret(), auth.GetIssuer(), auth.GetAudience(), auth.GetAccessTokenTtl().AsDuration())
	if err != nil {
		return platformruntime.Components{}, err
	}

	policy := accessinfra.NewPolicyRepo(enforcer, store)
	permissions := accessinterfaces.NewPermissionService(accessapp.NewPermissionUsecase(accessinfra.NewPermissionRepo(db), policy))
	roles := accessinterfaces.NewRoleBindingService(accessapp.NewRoleBindingUsecase(policy))
	dictionaries := dictionaryinterfaces.NewDictService(dictionaryinfra.NewDictRepo(db))
	sessions := authinfra.NewSessionRepository(db, issuer)
	login := authinterfaces.NewAuthService(authapp.NewUsecase(authinfra.NewProviderVerifier(auth), sessions, auth.GetAccessTokenTtl().AsDuration(), auth.GetRefreshTokenTtl().AsDuration()))
	hs := server.NewHTTPServer(bc.GetServer(), ms, func(s *http.Server) {
		accessv1.RegisterPermissionServiceHTTPServer(s, permissions)
		accessv1.RegisterRoleBindingServiceHTTPServer(s, roles)
		dictionaryv1.RegisterDictServiceHTTPServer(s, dictionaries)
		authv1.RegisterAuthServiceHTTPServer(s, login)
	})
	unregisterHealth := accessinfra.RegisterPolicyHealth(store, enforcer)
	stopReconciler := accessinfra.NewPolicyReconciler(store, enforcer, logger)
	ready = true
	return platformruntime.Components{
		Servers: []transport.Server{hs},
		Cleanup: func() { stopReconciler(); unregisterHealth(); closeDB() },
	}, nil
}
