package infrastructure

import (
	"context"
	"fmt"
	"log/slog"

	"google.golang.org/protobuf/proto"

	eventv1 "github.com/eagle-go/eagle/api/eagle/event/v1"
	notificationapp "github.com/eagle-go/eagle/app/admin/internal/notification/application"
	"github.com/eagle-go/eagle/pkg/messaging/rabbitmq"
	"github.com/eagle-go/eagle/pkg/platform/config"
)

func NewOrderCreatedConsumer(config *config.Messaging_RabbitMQ, usecase *notificationapp.Usecase, logger *slog.Logger) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		rabbitmq.RunConsumer(ctx, rabbitmq.Config{
			URL: config.GetUrl(), Exchange: config.GetExchange(),
			ReconnectBackoff: config.GetReconnectBackoff().AsDuration(),
		}, config.GetOrderCreatedQueue(), "order.created.v1", logger, func(ctx context.Context, message rabbitmq.Message) error {
			if message.Type != "eagle.event.v1.OrderCreatedV1" {
				return fmt.Errorf("notification: unexpected event type %q", message.Type)
			}
			var event eventv1.OrderCreatedV1
			if err := proto.Unmarshal(message.Body, &event); err != nil {
				return fmt.Errorf("notification: decode order-created event: %w", err)
			}
			if message.ID != "" && message.ID != event.GetEventId() {
				return fmt.Errorf("notification: message ID does not match payload")
			}
			return usecase.ReceiveOrderCreated(
				ctx, event.GetEventId(), event.GetOwnerSubject(), event.GetOrderId(), event.GetTotalCents(),
			)
		})
	}()
	return func() { cancel(); <-done }
}
