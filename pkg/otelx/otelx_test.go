package otelx

import (
	"context"
	"net"
	"slices"
	"testing"

	metricsdk "go.opentelemetry.io/otel/sdk/metric"
)

func TestMetricsListenerFailureIsSynchronous(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	shutdown, err := startMetricsServer(listener.Addr().String())
	if err == nil || shutdown != nil {
		if shutdown != nil {
			_ = shutdown(context.Background())
		}
		t.Fatalf("occupied health port accepted: %v", err)
	}
}

func TestMetricsListenerCanBeDisabled(t *testing.T) {
	shutdown, err := startMetricsServer("")
	if err != nil || shutdown != nil {
		t.Fatalf("disabled listener = %v, %v", shutdown == nil, err)
	}
}

func TestMetricsListenerStartsAndShutsDown(t *testing.T) {
	shutdown, err := startMetricsServer("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSecondsHistogramViewCoversSLOThreshold(t *testing.T) {
	stream, ok := secondsHistogramView("server_requests_seconds")(metricsdk.Instrument{Name: "server_requests_seconds"})
	if !ok {
		t.Fatal("request duration histogram view did not match")
	}
	aggregation, ok := stream.Aggregation.(metricsdk.AggregationExplicitBucketHistogram)
	if !ok {
		t.Fatalf("aggregation type = %T", stream.Aggregation)
	}
	if !slices.Contains(aggregation.Boundaries, 2.0) {
		t.Fatalf("histogram boundaries %v do not cover the 2s latency SLO", aggregation.Boundaries)
	}
}
