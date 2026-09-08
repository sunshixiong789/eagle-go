package server

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	kratoshttp "github.com/go-kratos/kratos/v3/transport/http"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/eagle-go/eagle/pkg/authn"
	"github.com/eagle-go/eagle/pkg/authz"
	"github.com/eagle-go/eagle/pkg/platform/config"
)

type denyAuthorizer struct{}

func (denyAuthorizer) AllowContext(context.Context, []string, string) (bool, error) {
	return false, nil
}

func TestNewVerifierAndMiddlewares(t *testing.T) {
	verifier := NewVerifier(&config.Auth{
		Issuer: "https://eagle.test", Audience: "eagle-api", SigningSecret: "test-signing-secret-at-least-32-bytes",
	})
	if verifier == nil {
		t.Fatal("verifier is nil")
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	middlewares, err := NewMiddlewares(logger, verifier, denyAuthorizer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(middlewares) != 10 {
		t.Fatalf("middleware count = %d", len(middlewares))
	}

	// Keep compile-time checks close to composition: these are the contracts the
	// server constructor accepts, not concrete Casbin or token implementations.
	var _ authz.Authorizer = denyAuthorizer{}
	var _ = authn.Config{}
}

func TestNewHTTPServerRegistersOwnedAPIs(t *testing.T) {
	registered := 0
	server := NewHTTPServer(&config.Server{Http: &config.Server_HTTP{
		Network: "tcp", Addr: "127.0.0.1:0", Timeout: durationpb.New(2 * time.Second),
	}}, nil, func(got *kratoshttp.Server) {
		if got == nil {
			t.Fatal("registrar received nil server")
		}
		registered++
	})
	if server == nil || registered != 1 {
		t.Fatalf("server=%v, registered=%d", server, registered)
	}

	// Empty optional settings must also use Kratos defaults without panicking.
	if got := NewHTTPServer(&config.Server{}, nil); got == nil {
		t.Fatal("default server is nil")
	}
}
