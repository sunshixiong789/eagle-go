package main

import (
	"log/slog"

	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"
	"github.com/go-kratos/kratos/v3/transport/http"
	"github.com/google/wire"

	accessv1 "github.com/eagle-go/eagle/api/eagle/access/v1"
	dictionaryv1 "github.com/eagle-go/eagle/api/eagle/dictionary/v1"
	accessapp "github.com/eagle-go/eagle/internal/access/application"
	accessinfra "github.com/eagle-go/eagle/internal/access/infrastructure"
	accessservice "github.com/eagle-go/eagle/internal/access/service"
	dictionaryinfra "github.com/eagle-go/eagle/internal/dictionary/infrastructure"
	dictionaryservice "github.com/eagle-go/eagle/internal/dictionary/service"
	platformdb "github.com/eagle-go/eagle/internal/platform/database"
	"github.com/eagle-go/eagle/pkg/authn"
	"github.com/eagle-go/eagle/pkg/authz"
	"github.com/eagle-go/eagle/pkg/platform/config"
	platformruntime "github.com/eagle-go/eagle/pkg/platform/runtime"
	"github.com/eagle-go/eagle/pkg/platform/server"
)

type policyHealth struct{}
type policyReconciler struct{}

var providerSet = wire.NewSet(
	provideData,
	provideAuth,
	provideServer,
	platformdb.Open,
	accessinfra.NewPolicyStore,
	accessinfra.NewEnforcer,
	accessinfra.NewPermissionRepo,
	accessinfra.NewPolicyRepo,
	provideAuthorizer,
	providePolicyHealth,
	providePolicyReconciler,
	accessapp.NewPermissionUsecase,
	accessapp.NewRoleBindingUsecase,
	accessservice.NewPermissionService,
	accessservice.NewRoleBindingService,
	dictionaryinfra.NewDictRepo,
	dictionaryservice.NewDictService,
	server.NewVerifier,
	provideMiddlewares,
	provideHTTPServer,
	newComponents,
)

// 默认构建不编译 wire.go，保留对 providerSet 的引用以免被标成未使用。
var _ = providerSet

func provideData(bc *config.Bootstrap) *config.Data     { return bc.GetData() }
func provideAuth(bc *config.Bootstrap) *config.Auth     { return bc.GetAuth() }
func provideServer(bc *config.Bootstrap) *config.Server { return bc.GetServer() }

func provideAuthorizer(enforcer *authz.Enforcer) authz.Authorizer {
	return enforcer
}

func providePolicyHealth(store *accessinfra.PolicyStore, enforcer *authz.Enforcer) (policyHealth, func(), error) {
	return policyHealth{}, accessinfra.RegisterPolicyHealth(store, enforcer), nil
}

func providePolicyReconciler(store *accessinfra.PolicyStore, enforcer *authz.Enforcer, logger *slog.Logger) (policyReconciler, func(), error) {
	return policyReconciler{}, accessinfra.NewPolicyReconciler(store, enforcer, logger), nil
}

func provideMiddlewares(
	logger *slog.Logger,
	verifier *authn.Verifier,
	authorizer authz.Authorizer,
	auth *config.Auth,
) ([]middleware.Middleware, error) {
	return server.NewMiddlewares(logger, verifier, authorizer, auth, errorMappings()...)
}

func provideHTTPServer(
	c *config.Server,
	ms []middleware.Middleware,
	permission *accessservice.PermissionService,
	role *accessservice.RoleBindingService,
	dict *dictionaryservice.DictService,
) *http.Server {
	return server.NewHTTPServer(c, ms, func(s *http.Server) {
		accessv1.RegisterPermissionServiceHTTPServer(s, permission)
		accessv1.RegisterRoleBindingServiceHTTPServer(s, role)
		dictionaryv1.RegisterDictServiceHTTPServer(s, dict)
	})
}

func newComponents(
	hs *http.Server,
	_ policyHealth,
	_ policyReconciler,
) platformruntime.Components {
	return platformruntime.Components{Servers: []transport.Server{hs}}
}
