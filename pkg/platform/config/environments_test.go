package config_test

import (
	"strconv"
	"testing"
	"time"

	kratosconfig "github.com/go-kratos/kratos/v3/config"
	configenv "github.com/go-kratos/kratos/v3/config/env"
	"github.com/go-kratos/kratos/v3/config/file"

	appconfig "github.com/eagle-go/eagle/pkg/platform/config"
)

func TestSharedTemplateWithEnvironmentVariables(t *testing.T) {
	for _, tt := range []struct {
		name  string
		level string
		ratio string
	}{
		{"development", "debug", "1.0"},
		{"testing", "info", "0.1"},
		{"production", "info", "0.05"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("EAGLE_AUTH_ISSUER", "https://auth."+tt.name+".example.com")
			t.Setenv("EAGLE_AUTH_AUDIENCE", "eagle-api-"+tt.name)
			t.Setenv("EAGLE_AUTH_ACTIVE_SIGNING_KEY_ID", tt.name+"-key")
			t.Setenv("EAGLE_AUTH_SIGNING_KEY_DIRECTORY", "/run/secrets/eagle-jwt")
			t.Setenv("EAGLE_OBSERVABILITY_LOG_LEVEL", tt.level)
			t.Setenv("EAGLE_OBSERVABILITY_TRACE_SAMPLE_RATIO", tt.ratio)
			t.Setenv("EAGLE_OBSERVABILITY_METRICS_ADDR", "0.0.0.0:9100")
			t.Setenv("EAGLE_SERVER_HTTP_TIMEOUT", "8s")
			t.Setenv("EAGLE_SERVER_SWAGGER_ENABLED", strconv.FormatBool(tt.name == "development"))
			t.Setenv("EAGLE_DATABASE_MAX_CONNS", "40")
			t.Setenv("EAGLE_DATABASE_MAX_IDLE_CONNS", "4")
			t.Setenv("EAGLE_DATABASE_MAX_CONN_LIFETIME", "7200s")
			t.Setenv("EAGLE_DATABASE_MAX_CONN_IDLE_TIME", "600s")
			for _, driver := range []string{"postgres", "mysql"} {
				t.Run(driver, func(t *testing.T) {
					dsn := "postgres://eagle:fixture@db.internal/" + tt.name + "?sslmode=require"
					if driver == "mysql" {
						dsn = "eagle:fixture@tcp(db.internal:3306)/" + tt.name + "?parseTime=true&tls=true"
					}
					t.Setenv("EAGLE_DATABASE_DRIVER", driver)
					t.Setenv("EAGLE_DATABASE_DSN", dsn)
					c := kratosconfig.New(
						kratosconfig.WithSource(file.NewSource(configDir(t)), configenv.NewSource("EAGLE")),
						kratosconfig.WithResolveActualTypes(true),
					)
					t.Cleanup(func() { _ = c.Close() })
					if err := c.Load(); err != nil {
						t.Fatal(err)
					}
					var bc appconfig.Bootstrap
					if err := c.Scan(&bc); err != nil {
						t.Fatal(err)
					}
					if err := appconfig.Validate(&bc); err != nil {
						t.Fatal(err)
					}
					if bc.GetServer().GetSwagger().GetEnabled() != (tt.name == "development") {
						t.Fatal("Swagger must only be enabled in development")
					}
					db := bc.GetData().GetDatabase()
					if db.GetDsn() != dsn || db.GetDriver() != driver || db.GetMaxConns() != 40 || db.GetMaxIdleConns() != 4 {
						t.Fatal("database variables did not override shared template")
					}
					if db.GetMaxConnLifetime().AsDuration() != 2*time.Hour || db.GetMaxConnIdleTime().AsDuration() != 10*time.Minute || bc.GetServer().GetHttp().GetTimeout().AsDuration() != 8*time.Second {
						t.Fatal("duration variables did not override shared template")
					}
					if bc.GetAuth().GetAudience() != "eagle-api-"+tt.name || bc.GetAuth().GetActiveSigningKeyId() != tt.name+"-key" {
						t.Fatal("authentication settings leaked between environments")
					}
					if bc.GetObservability().GetLogLevel() != tt.level || bc.GetObservability().GetMetricsAddr() != "0.0.0.0:9100" {
						t.Fatal("observability variables did not override shared template")
					}
					wantRatio, err := strconv.ParseFloat(tt.ratio, 64)
					if err != nil || bc.GetObservability().GetTraceSampleRatio() != wantRatio {
						t.Fatal("trace sampling variable did not override shared template")
					}
				})
			}
		})
	}
}
