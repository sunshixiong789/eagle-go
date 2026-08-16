package authn

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var revocationMetrics struct {
	once   sync.Once
	checks metric.Int64Counter
}

func recordRevocationCheck(ctx context.Context, outcome string) {
	revocationMetrics.once.Do(func() {
		revocationMetrics.checks, _ = otel.Meter("github.com/eagle-go/eagle/pkg/authn").Int64Counter("eagle.authn.revocation_checks")
	})
	if revocationMetrics.checks != nil {
		revocationMetrics.checks.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
	}
}
