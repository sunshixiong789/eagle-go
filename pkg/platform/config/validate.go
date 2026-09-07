package config

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
)

type Requirements struct {
	Database bool
	Auth     bool
	HTTP     bool
}

// Validate 校验部署时最终合并出的配置。requirements 为空时校验示例配置
// 的全部字段；进程只声明自己真实使用的可选配置。
func Validate(b *Bootstrap, requirements ...Requirements) error {
	if b == nil {
		return errors.New("config: bootstrap is nil")
	}
	var errs []error
	db := b.GetData().GetDatabase()
	auth := b.GetAuth()
	server := b.GetServer()
	obs := b.GetObservability()
	required := Requirements{Database: true, Auth: true, HTTP: true}
	if len(requirements) > 0 {
		required = requirements[0]
	}

	if required.Database && db.GetDsn() == "" {
		errs = append(errs, errors.New("data.database.dsn is required"))
	} else if required.Database {
		if u, err := url.Parse(db.GetDsn()); err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
			errs = append(errs, errors.New("data.database.dsn must be a postgres URL"))
		}
	}
	if required.Database && (db.GetMaxConns() <= 0 || db.GetMaxIdleConns() < 0 || db.GetMaxIdleConns() > db.GetMaxConns()) {
		errs = append(errs, errors.New("database pool requires 0 <= max_idle_conns <= max_conns and max_conns > 0"))
	}
	if required.Auth {
		if err := validateIssuer(auth.GetIssuer()); err != nil {
			errs = append(errs, err)
		}
		if auth.GetSigningSecret() == "" {
			if err := validateJWKS(auth.GetJwksUrl(), auth.GetJwksPath()); err != nil {
				errs = append(errs, err)
			}
		} else {
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
		}
		if auth.GetGoogle().GetEnabled() && auth.GetGoogle().GetClientId() == "" {
			errs = append(errs, errors.New("auth.google.client_id is required when Google login is enabled"))
		}
		if auth.GetApple().GetEnabled() && auth.GetApple().GetClientId() == "" {
			errs = append(errs, errors.New("auth.apple.client_id is required when Apple login is enabled"))
		}
		if auth.GetClientId() == "" || auth.GetAudience() == "" {
			errs = append(errs, errors.New("auth.client_id and auth.audience are required"))
		}
		if auth.GetClientRolesClaim() == "" {
			errs = append(errs, errors.New("auth.client_roles_claim is required"))
		}
		if auth.GetSuperAdminRole() == "" {
			errs = append(errs, errors.New("auth.super_admin_role is required"))
		}
	}
	if required.HTTP && server.GetHttp().GetAddr() == "" {
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

// validateJWKS 校验两种取 JWKS 的方式：显式 jwks_url，或 issuer + jwks_path。
func validateJWKS(jwksURL, jwksPath string) error {
	if jwksURL != "" {
		u, err := url.Parse(jwksURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return errors.New("auth.jwks_url must be an absolute URL with scheme and host")
		}
		return nil
	}
	if jwksPath == "" {
		return errors.New("auth.jwks_url or auth.jwks_path is required")
	}
	if !strings.HasPrefix(jwksPath, "/") {
		return errors.New("auth.jwks_path must start with /")
	}
	return nil
}
