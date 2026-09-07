package config

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
)

// Validate 校验单体进程启动所需的完整配置。
func Validate(b *Bootstrap) error {
	if b == nil {
		return errors.New("config: bootstrap is nil")
	}
	var errs []error
	db := b.GetData().GetDatabase()
	auth := b.GetAuth()
	server := b.GetServer()
	obs := b.GetObservability()

	if db.GetDsn() == "" {
		errs = append(errs, errors.New("data.database.dsn is required"))
	} else {
		if u, err := url.Parse(db.GetDsn()); err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
			errs = append(errs, errors.New("data.database.dsn must be a postgres URL"))
		}
	}
	if db.GetMaxConns() <= 0 || db.GetMaxIdleConns() < 0 || db.GetMaxIdleConns() > db.GetMaxConns() {
		errs = append(errs, errors.New("database pool requires 0 <= max_idle_conns <= max_conns and max_conns > 0"))
	}
	if err := validateIssuer(auth.GetIssuer()); err != nil {
		errs = append(errs, err)
	}
	if auth.GetAudience() == "" {
		errs = append(errs, errors.New("auth.audience is required"))
	}
	if len(auth.GetSigningSecret()) < 32 {
		errs = append(errs, errors.New("auth.signing_secret must be at least 32 bytes"))
	}
	accessTTL := protoDuration(auth.GetAccessTokenTtl())
	refreshTTL := protoDuration(auth.GetRefreshTokenTtl())
	if accessTTL <= 0 || refreshTTL <= 0 {
		errs = append(errs, errors.New("auth access_token_ttl and refresh_token_ttl must be positive"))
	} else if refreshTTL <= accessTTL {
		errs = append(errs, errors.New("auth.refresh_token_ttl must exceed access_token_ttl"))
	}
	if auth.GetGoogle().GetEnabled() && auth.GetGoogle().GetClientId() == "" {
		errs = append(errs, errors.New("auth.google.client_id is required when Google login is enabled"))
	}
	if auth.GetApple().GetEnabled() && auth.GetApple().GetClientId() == "" {
		errs = append(errs, errors.New("auth.apple.client_id is required when Apple login is enabled"))
	}
	if server.GetHttp().GetAddr() == "" {
		errs = append(errs, errors.New("server.http.addr is required"))
	}
	if obs.GetTraceSampleRatio() < 0 || obs.GetTraceSampleRatio() > 1 {
		errs = append(errs, errors.New("observability.trace_sample_ratio must be in [0,1]"))
	}
	if obs.GetMetricsAddr() != "" && obs.GetMetricsAddr() == server.GetHttp().GetAddr() {
		errs = append(errs, errors.New("observability.metrics_addr must differ from server.http.addr"))
	}
	return errors.Join(errs...)
}

func protoDuration(d *durationpb.Duration) time.Duration {
	if d == nil {
		return 0
	}
	return d.AsDuration()
}

// validateIssuer 只要求 issuer 是一个不带尾斜杠的绝对 URL。
//
// 这里刻意不校验路径形态：不同 IdP 的 issuer 可能是裸域名或任意路径。
func validateIssuer(raw string) error {
	if raw == "" {
		return errors.New("auth.issuer is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return errors.New("auth.issuer must be an absolute URL with scheme and host")
	}
	if strings.HasSuffix(raw, "/") {
		return errors.New("auth.issuer must not have a trailing slash")
	}
	return nil
}
