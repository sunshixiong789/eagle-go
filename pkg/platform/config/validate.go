package config

import (
	"errors"
	"net/url"
	"strings"
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
		if err := validateJWKS(auth.GetJwksUrl(), auth.GetJwksPath()); err != nil {
			errs = append(errs, err)
		}
		if auth.GetClientId() == "" || auth.GetAudience() == "" {
			errs = append(errs, errors.New("auth.client_id and auth.audience are required"))
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

// validateIssuer 只要求 issuer 是一个不带尾斜杠的绝对 URL。
//
// 这里刻意不校验路径形态：Keycloak 是 /realms/<realm>，Auth0 是裸域名，
// Logto 是 /oidc，Authing 又是另一套。把 Keycloak 的路径约定写死在校验里，
// 换 IdP 时进程会直接启动失败，而这与「IdP 可替换」的设计目标冲突。
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
	if jwksPath != "" && !strings.HasPrefix(jwksPath, "/") {
		return errors.New("auth.jwks_path must start with /")
	}
	return nil
}
