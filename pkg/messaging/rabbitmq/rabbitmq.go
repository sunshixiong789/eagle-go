// Package rabbitmq provides reliable publish confirms and reconnecting consumers.
package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Config struct {
	URL              string
	Exchange         string
	ReconnectBackoff time.Duration
}

type Message struct {
	ID         string
	Type       string
	RoutingKey string
	Body       []byte
	Timestamp  time.Time
	Headers    amqp.Table
}

type Publisher struct {
	config Config
	mu     sync.Mutex
	conn   *amqp.Connection
	ch     *amqp.Channel
}

func NewPublisher(config Config) (*Publisher, error) {
	if config.URL == "" || config.Exchange == "" {
		return nil, errors.New("rabbitmq: URL and exchange are required")
	}
	return &Publisher{config: config}, nil
}

func (p *Publisher) Publish(ctx context.Context, message Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.ensureConnected(); err != nil {
		return err
	}
	confirmation, err := p.ch.PublishWithDeferredConfirmWithContext(ctx, p.config.Exchange, message.RoutingKey, false, false, amqp.Publishing{
		Headers: message.Headers, ContentType: "application/protobuf", DeliveryMode: amqp.Persistent,
		MessageId: message.ID, Type: message.Type, Timestamp: message.Timestamp, Body: message.Body,
	})
	if err != nil {
		p.reset()
		return fmt.Errorf("rabbitmq: publish: %w", err)
	}
	acked, err := confirmation.WaitContext(ctx)
	if err != nil {
		p.reset()
		return fmt.Errorf("rabbitmq: wait publisher confirm: %w", err)
	}
	if !acked {
		return errors.New("rabbitmq: broker rejected published message")
	}
	return nil
}

func (p *Publisher) ensureConnected() error {
	if p.conn != nil && !p.conn.IsClosed() && p.ch != nil && !p.ch.IsClosed() {
		return nil
	}
	p.reset()
	conn, err := amqp.Dial(p.config.URL)
	if err != nil {
		return fmt.Errorf("rabbitmq: connect publisher: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("rabbitmq: open publisher channel: %w", err)
	}
	if err := ch.ExchangeDeclare(p.config.Exchange, "topic", true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("rabbitmq: declare exchange: %w", err)
	}
	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("rabbitmq: enable publisher confirms: %w", err)
	}
	p.conn, p.ch = conn, ch
	return nil
}

func (p *Publisher) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reset()
}

func (p *Publisher) reset() {
	if p.ch != nil {
		_ = p.ch.Close()
	}
	if p.conn != nil {
		_ = p.conn.Close()
	}
	p.ch, p.conn = nil, nil
}

type Handler func(context.Context, Message) error

// RunConsumer reconnects until ctx is cancelled. A message is retried once,
// then dead-lettered so a poison event cannot block the queue indefinitely.
func RunConsumer(ctx context.Context, config Config, queue, routingKey string, logger *slog.Logger, handler Handler) {
	for ctx.Err() == nil {
		err := consume(ctx, config, queue, routingKey, handler)
		if ctx.Err() != nil {
			return
		}
		logger.ErrorContext(ctx, "RabbitMQ consumer disconnected", "queue", queue, "error", err)
		timer := time.NewTimer(config.ReconnectBackoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func consume(ctx context.Context, config Config, queue, routingKey string, handler Handler) error {
	conn, err := amqp.Dial(config.URL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = conn.Close() }()
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open channel: %w", err)
	}
	defer func() { _ = ch.Close() }()
	if err := declareConsumerTopology(ch, config.Exchange, queue, routingKey); err != nil {
		return err
	}
	if err := ch.Qos(32, 0, false); err != nil {
		return fmt.Errorf("set qos: %w", err)
	}
	deliveries, err := ch.Consume(queue, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case delivery, ok := <-deliveries:
			if !ok {
				return errors.New("delivery channel closed")
			}
			message := Message{
				ID: delivery.MessageId, Type: delivery.Type, RoutingKey: delivery.RoutingKey,
				Body: delivery.Body, Timestamp: delivery.Timestamp, Headers: delivery.Headers,
			}
			if err := handler(ctx, message); err != nil {
				if nackErr := delivery.Nack(false, !delivery.Redelivered); nackErr != nil {
					return fmt.Errorf("nack message: %w", errors.Join(err, nackErr))
				}
				continue
			}
			if err := delivery.Ack(false); err != nil {
				return fmt.Errorf("ack message: %w", err)
			}
		}
	}
}

func declareConsumerTopology(ch *amqp.Channel, exchange, queue, routingKey string) error {
	if err := ch.ExchangeDeclare(exchange, "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange: %w", err)
	}
	dlx := exchange + ".dlx"
	if err := ch.ExchangeDeclare(dlx, "fanout", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dead-letter exchange: %w", err)
	}
	dlq := queue + ".dlq"
	if _, err := ch.QueueDeclare(dlq, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dead-letter queue: %w", err)
	}
	if err := ch.QueueBind(dlq, "", dlx, false, nil); err != nil {
		return fmt.Errorf("bind dead-letter queue: %w", err)
	}
	args := amqp.Table{"x-dead-letter-exchange": dlx}
	if _, err := ch.QueueDeclare(queue, true, false, false, false, args); err != nil {
		return fmt.Errorf("declare queue: %w", err)
	}
	if err := ch.QueueBind(queue, routingKey, exchange, false, nil); err != nil {
		return fmt.Errorf("bind queue: %w", err)
	}
	return nil
}
