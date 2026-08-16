package redisx

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var cacheMetrics struct {
	once       sync.Once
	operations metric.Int64Counter
}

func recordCache(ctx context.Context, outcome string) {
	cacheMetrics.once.Do(func() {
		cacheMetrics.operations, _ = otel.Meter("github.com/eagle-go/eagle/pkg/redisx").Int64Counter("eagle.cache.operations")
	})
	if cacheMetrics.operations != nil {
		cacheMetrics.operations.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
	}
}
