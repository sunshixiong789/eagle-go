package conf

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Validate 校验部署时最终合并出的配置，而不只校验仓库里的示例文件。
func Validate(b *Bootstrap) error {
	if b == nil {
		return errors.New("config: bootstrap is nil")
	}
	var errs []error
	db := b.GetData().GetDatabase()
	redis := b.GetData().GetRedis()
	auth := b.GetAuth()
	server := b.GetServer()
	obs := b.GetObservability()

	if db.GetDsn() == "" {
		errs = append(errs, errors.New("data.database.dsn is required"))
	} else if u, err := url.Parse(db.GetDsn()); err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		errs = append(errs, errors.New("data.database.dsn must be a postgres URL"))
	}
	if db.GetMaxConns() <= 0 || db.GetMinConns() < 0 || db.GetMinConns() > db.GetMaxConns() {
		errs = append(errs, errors.New("database pool requires 0 <= min_conns <= max_conns and max_conns > 0"))
	}
	if redis.GetAddr() == "" {
		errs = append(errs, errors.New("data.redis.addr is required"))
	} else if _, _, err := net.SplitHostPort(redis.GetAddr()); err != nil {
		errs = append(errs, fmt.Errorf("data.redis.addr: %w", err))
	}
	if err := validateIssuer(auth.GetIssuer()); err != nil {
		errs = append(errs, err)
	}
	if auth.GetClientId() == "" || auth.GetAudience() == "" {
		errs = append(errs, errors.New("auth.client_id and auth.audience are required"))
	}
	if auth.GetSuperAdminRole() == "" {
		errs = append(errs, errors.New("auth.super_admin_role is required"))
	}
	if server.GetHttp().GetAddr() == "" || server.GetGrpc().GetAddr() == "" {
		errs = append(errs, errors.New("server.http.addr and server.grpc.addr are required"))
	}
	if obs.GetTraceSampleRatio() < 0 || obs.GetTraceSampleRatio() > 1 {
		errs = append(errs, errors.New("observability.trace_sample_ratio must be in [0,1]"))
	}
	if obs.GetMetricsAddr() != "" && obs.GetMetricsAddr() == server.GetHttp().GetAddr() {
		errs = append(errs, errors.New("observability.metrics_addr must differ from server.http.addr"))
	}
	return errors.Join(errs...)
}

func validateIssuer(raw string) error {
	if raw == "" {
		return errors.New("auth.issuer is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" || !strings.Contains(u.Path, "/realms/") {
		return errors.New("auth.issuer must be an absolute Keycloak realm URL")
	}
	if strings.HasSuffix(raw, "/") {
		return errors.New("auth.issuer must not have a trailing slash")
	}
	return nil
}
