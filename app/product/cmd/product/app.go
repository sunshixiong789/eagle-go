package main

import (
	"log/slog"

	"github.com/go-kratos/kratos/v3/transport"
	"github.com/go-kratos/kratos/v3/transport/grpc"
	"github.com/go-kratos/kratos/v3/transport/http"

	productv1 "github.com/eagle-go/eagle/api/eagle/product/v1"
	platformdb "github.com/eagle-go/eagle/app/product/internal/platform/database"
	productapp "github.com/eagle-go/eagle/app/product/internal/product/application"
	productdomain "github.com/eagle-go/eagle/app/product/internal/product/domain"
	productinfra "github.com/eagle-go/eagle/app/product/internal/product/infrastructure"
	accessclient "github.com/eagle-go/eagle/app/product/internal/product/infrastructure/accessclient"
	productservice "github.com/eagle-go/eagle/app/product/internal/product/service"
	"github.com/eagle-go/eagle/pkg/platform/config"
	platformruntime "github.com/eagle-go/eagle/pkg/platform/runtime"
	"github.com/eagle-go/eagle/pkg/platform/server"
)

func buildApp(bc *config.Bootstrap, logger *slog.Logger) (platformruntime.Components, error) {
	db, closeDB, err := platformdb.Open(bc.GetData())
	if err != nil {
		return platformruntime.Components{}, err
	}
	authorizer, closeAuthorizer, err := accessclient.NewAuthorizer(bc.GetUpstream())
	if err != nil {
		closeDB()
		return platformruntime.Components{}, err
	}
	service := productservice.NewProductService(productapp.NewUsecase(productinfra.NewRepository(db)))
	ms, err := server.NewMiddlewares(
		logger, server.NewVerifier(bc.GetAuth()), authorizer, bc.GetAuth(),
		server.NotFound(productdomain.ErrProductNotFound, productv1.ErrorReason_ERROR_REASON_PRODUCT_NOT_FOUND),
		server.Conflict(productdomain.ErrProductSKUDuplicated, productv1.ErrorReason_ERROR_REASON_PRODUCT_SKU_DUPLICATED),
	)
	if err != nil {
		closeAuthorizer()
		closeDB()
		return platformruntime.Components{}, err
	}
	gs := server.NewGRPCServer(bc.GetServer(), ms, func(s *grpc.Server) { productv1.RegisterProductServiceServer(s, service) })
	hs := server.NewHTTPServer(bc.GetServer(), ms, nil, func(s *http.Server) { productv1.RegisterProductServiceHTTPServer(s, service) })
	return platformruntime.Components{
		Servers: []transport.Server{gs, hs},
		Cleanup: func() { closeAuthorizer(); closeDB() },
	}, nil
}
