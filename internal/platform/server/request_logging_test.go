package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	kratoserrors "github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/middleware"

	authv1 "github.com/eagle-go/eagle/api/eagle/auth/v1"
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

func TestRequestLoggingHidesCredentialsByDefault(t *testing.T) {
	const credential = "synthetic-sensitive-credential"
	const nonce = "synthetic-sensitive-nonce"
	requests := map[string]any{
		"login":   &authv1.SocialLoginRequest{IdToken: credential, Nonce: nonce},
		"refresh": &authv1.RefreshTokenRequest{RefreshToken: credential},
		"logout":  &authv1.LogoutRequest{RefreshToken: credential},
		"struct":  struct{ Password string }{Password: credential},
		"string":  credential,
	}
	for name, req := range requests {
		for _, failed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/failed=%t", name, failed), func(t *testing.T) {
				var output bytes.Buffer
				logger := slog.New(slog.NewJSONHandler(&output, nil))
				handler := RequestLogging(logger)(func(context.Context, any) (any, error) {
					if failed {
						return nil, kratoserrors.Unauthorized("UNAUTHENTICATED", "invalid credential")
					}
					return nil, nil
				})
				_, _ = handler(context.Background(), req)
				if output.Len() == 0 {
					t.Fatal("request log is missing")
				}
				for _, secret := range []string{credential, nonce} {
					if strings.Contains(output.String(), secret) {
						t.Fatal("request log contains a credential")
					}
				}
			})
		}
	}
}

func TestRecoveryLoggingHidesCredentials(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	middlewares, err := NewMiddlewares(logger, nil, denyAuthorizer{})
	if err != nil {
		t.Fatal(err)
	}
	handler := middlewares[0](func(context.Context, any) (any, error) {
		panic("synthetic failure")
	})
	const credential = "synthetic-sensitive-credential"
	_, err = handler(context.Background(), &authv1.SocialLoginRequest{IdToken: credential, Nonce: credential})
	if err == nil || kratoserrors.FromError(err).Code != 500 {
		t.Fatalf("recovered error = %v", err)
	}
	if strings.Contains(output.String(), credential) {
		t.Fatal("recovery log contains a credential")
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	stack, _ := record["stack"].(string)
	if record["request"] != "[REDACTED]" || stack == "" || record["level"] != "ERROR" {
		t.Fatalf("unexpected recovery log: %v", record)
	}
}
