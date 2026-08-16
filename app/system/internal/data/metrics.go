package data

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var policySyncMetrics struct {
	once         sync.Once
	outboxEvents metric.Int64Counter
	versionLag   metric.Int64Histogram
}

func initPolicySyncMetrics() {
	policySyncMetrics.once.Do(func() {
		meter := otel.Meter("github.com/eagle-go/eagle/app/system/internal/data")
		policySyncMetrics.outboxEvents, _ = meter.Int64Counter("eagle.authz.outbox_events")
		policySyncMetrics.versionLag, _ = meter.Int64Histogram("eagle.authz.policy_version_lag")
	})
}

func recordOutboxEvent(ctx context.Context, outcome string) {
	initPolicySyncMetrics()
	if policySyncMetrics.outboxEvents != nil {
		policySyncMetrics.outboxEvents.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
	}
}

func recordPolicyVersionLag(ctx context.Context, databaseVersion, loadedVersion int64) {
	initPolicySyncMetrics()
	lag := databaseVersion - loadedVersion
	if lag < 0 {
		lag = 0
	}
	if policySyncMetrics.versionLag != nil {
		policySyncMetrics.versionLag.Record(ctx, lag)
	}
}
