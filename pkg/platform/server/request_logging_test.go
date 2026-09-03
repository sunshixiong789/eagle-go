package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	kratoserrors "github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/middleware"
)

func TestRequestLoggingLevels(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantLevel string
		wantStack bool
	}{
		{name: "success", wantLevel: "INFO"},
		{name: "unauthenticated", err: kratoserrors.Unauthorized("UNAUTHENTICATED", "missing token"), wantLevel: "INFO"},
		{name: "forbidden", err: kratoserrors.Forbidden("FORBIDDEN", "missing permission"), wantLevel: "INFO"},
		{name: "bad request", err: kratoserrors.BadRequest("BAD_REQUEST", "invalid request"), wantLevel: "WARN"},
		{name: "internal", err: errors.New("database unavailable"), wantLevel: "ERROR", wantStack: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))
			handler := RequestLogging(logger)(func(context.Context, any) (any, error) {
				return nil, tc.err
			})
			if _, err := handler(context.Background(), "request"); !errors.Is(err, tc.err) {
				t.Fatalf("handler error = %v, want %v", err, tc.err)
			}

			var record map[string]any
			if err := json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatalf("decode log record: %v", err)
			}
			if got := record["level"]; got != tc.wantLevel {
				t.Errorf("level = %v, want %s", got, tc.wantLevel)
			}
			_, hasStack := record["stack"]
			if hasStack != tc.wantStack {
				t.Errorf("stack present = %v, want %v", hasStack, tc.wantStack)
			}
		})
	}
}

func TestRequestLoggingUsesRedactedArgs(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	handler := RequestLogging(logger)(middleware.Handler(func(context.Context, any) (any, error) {
		return nil, nil
	}))
	if _, err := handler(context.Background(), redactedRequest{}); err != nil {
		t.Fatal(err)
	}

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if got := record["args"]; got != "[REDACTED]" {
		t.Errorf("args = %v, want [REDACTED]", got)
	}
}

type redactedRequest struct{}

func (redactedRequest) Redact() string { return "[REDACTED]" }
