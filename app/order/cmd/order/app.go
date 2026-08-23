package main

import (
	"log/slog"

	"github.com/go-kratos/kratos/v3/transport"
	"github.com/go-kratos/kratos/v3/transport/grpc"
	"github.com/go-kratos/kratos/v3/transport/http"

	orderv1 "github.com/eagle-go/eagle/api/eagle/order/v1"
	orderapp "github.com/eagle-go/eagle/app/order/internal/order/application"
	orderdomain "github.com/eagle-go/eagle/app/order/internal/order/domain"
	orderinfra "github.com/eagle-go/eagle/app/order/internal/order/infrastructure"
	orderservice "github.com/eagle-go/eagle/app/order/internal/order/service"
	platformdb "github.com/eagle-go/eagle/app/order/internal/platform/database"
	"github.com/eagle-go/eagle/pkg/messaging/rabbitmq"
	"github.com/eagle-go/eagle/pkg/platform/config"
	platformruntime "github.com/eagle-go/eagle/pkg/platform/runtime"
	"github.com/eagle-go/eagle/pkg/platform/server"
)

func buildApp(bc *config.Bootstrap, logger *slog.Logger) (platformruntime.Components, error) {
	db, closeDB, err := platformdb.Open(bc.GetData())
	if err != nil {
		return platformruntime.Components{}, err
	}
	products, closeProducts, err := orderinfra.NewProductClient(bc.GetUpstream(), bc.GetServiceAuth())
	if err != nil {
		closeDB()
		return platformruntime.Components{}, err
	}
	rabbitConfig := bc.GetMessaging().GetRabbitmq()
	publisher, err := rabbitmq.NewPublisher(rabbitmq.Config{
		URL: rabbitConfig.GetUrl(), Exchange: rabbitConfig.GetExchange(),
		ReconnectBackoff: rabbitConfig.GetReconnectBackoff().AsDuration(),
	})
	if err != nil {
		closeProducts()
		closeDB()
		return platformruntime.Components{}, err
	}
	stopOutboxRelay := orderinfra.NewOutboxRelay(db, publisher, logger)
	service := orderservice.NewOrderService(orderapp.NewUsecase(orderinfra.NewRepository(db), products))
	ms, err := server.NewMiddlewares(
		logger, server.NewVerifier(bc.GetAuth()), nil, bc.GetAuth(),
		server.NotFound(orderdomain.ErrOrderNotFound, orderv1.ErrorReason_ERROR_REASON_ORDER_NOT_FOUND),
		server.BadRequest(orderdomain.ErrProductUnavailable, orderv1.ErrorReason_ERROR_REASON_PRODUCT_UNAVAILABLE),
		server.BadRequest(orderdomain.ErrInvalidOrder, orderv1.ErrorReason_ERROR_REASON_INVALID_ORDER),
	)
	if err != nil {
		stopOutboxRelay()
		publisher.Close()
		closeProducts()
		closeDB()
		return platformruntime.Components{}, err
	}
	gs := server.NewGRPCServer(bc.GetServer(), ms, func(s *grpc.Server) { orderv1.RegisterOrderServiceServer(s, service) })
	hs := server.NewHTTPServer(bc.GetServer(), ms, nil, func(s *http.Server) { orderv1.RegisterOrderServiceHTTPServer(s, service) })
	return platformruntime.Components{
		Servers: []transport.Server{gs, hs},
		Cleanup: func() { stopOutboxRelay(); publisher.Close(); closeProducts(); closeDB() },
	}, nil
}
