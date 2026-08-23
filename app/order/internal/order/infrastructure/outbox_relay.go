package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	platformdb "github.com/eagle-go/eagle/app/order/internal/platform/database"
	"github.com/eagle-go/eagle/pkg/messaging/rabbitmq"
)

type outboxRecord struct {
	id, eventType, routingKey string
	payload                   []byte
	createdAt                 time.Time
	attempts                  int32
}

func NewOutboxRelay(db *platformdb.Database, publisher *rabbitmq.Publisher, logger *slog.Logger) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runOutboxRelay(ctx, db.SQL(), publisher, logger)
	}()
	return func() { cancel(); <-done }
}

func runOutboxRelay(ctx context.Context, db *sql.DB, publisher *rabbitmq.Publisher, logger *slog.Logger) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		for {
			record, err := claimOutbox(ctx, db)
			if errors.Is(err, sql.ErrNoRows) {
				break
			}
			if err != nil {
				if ctx.Err() == nil {
					logger.ErrorContext(ctx, "claim order outbox failed", "error", err)
				}
				break
			}
			err = publisher.Publish(ctx, rabbitmq.Message{
				ID: record.id, Type: record.eventType, RoutingKey: record.routingKey,
				Body: record.payload, Timestamp: record.createdAt,
			})
			if err != nil {
				_ = releaseOutbox(ctx, db, record.id, record.attempts, err.Error())
				logger.WarnContext(ctx, "publish order event failed", "event_id", record.id, "error", err)
				break
			}
			if _, err := db.ExecContext(ctx, `UPDATE event_outbox SET published_at = now(), locked_until = NULL, last_error = '' WHERE id = $1`, record.id); err != nil {
				logger.ErrorContext(ctx, "mark order event published failed", "event_id", record.id, "error", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func claimOutbox(ctx context.Context, db *sql.DB) (*outboxRecord, error) {
	const query = `
WITH candidate AS (
    SELECT id FROM event_outbox
    WHERE published_at IS NULL AND available_at <= now()
      AND (locked_until IS NULL OR locked_until < now())
    ORDER BY created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE event_outbox AS e
SET locked_until = now() + interval '30 seconds', attempts = attempts + 1
FROM candidate
WHERE e.id = candidate.id
RETURNING e.id, e.event_type, e.routing_key, e.payload, e.created_at, e.attempts`
	var record outboxRecord
	err := db.QueryRowContext(ctx, query).Scan(
		&record.id, &record.eventType, &record.routingKey, &record.payload, &record.createdAt, &record.attempts,
	)
	return &record, err
}

func releaseOutbox(ctx context.Context, db *sql.DB, id string, attempts int32, message string) error {
	if len(message) > 1024 {
		message = message[:1024]
	}
	delay := time.Duration(attempts)
	if delay > 60 {
		delay = 60
	}
	_, err := db.ExecContext(ctx, `
UPDATE event_outbox
SET locked_until = NULL, available_at = now() + ($2 * interval '1 second'), last_error = $3
WHERE id = $1`, id, delay, message)
	return err
}
