// Package authn 是 OAuth2 资源服务器侧的认证中间件。
//
// 它验证 access token 的签名（经认证中心的 JWKS）、时间与 issuer 声明，
// 再把 identity.Principal 放进 context 交给 authz 判定授权。
//
// 签名验证在本地完成，不需要为每个请求回调认证中心；
package authn

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	kratoserrors "github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"

	"github.com/eagle-go/eagle/pkg/identity"
)

// 认证相关错误。
//
// 这些是内部哨兵值，供 errors.Is 判定；它们不会直接出现在响应里，
// 中间件会在传输层边界把它们翻译成带 reason 的 kratos 错误。
var (
	ErrInvalidToken = errors.New("authn: token 无效")
	ErrTokenExpired = errors.New("authn: token 已过期")
)

// 认证失败的 reason，客户端据此决定「刷新 token」还是「重新登录」。
const (
	ReasonUnauthenticated = "UNAUTHENTICATED"
	ReasonTokenExpired    = "TOKEN_EXPIRED"
)

// DefaultJWKSPath 是 Keycloak 的 JWKS 路径。
// 注意它不是 OIDC 常见的 /.well-known/jwks.json——Auth0、Logto 用后者，
// 所以这个路径必须可配置。
const DefaultJWKSPath = "/protocol/openid-connect/certs"

// Config 是资源服务器的验证参数。
type Config struct {
	// Issuer 必须与 IdP 签发的 iss 完全一致，
	// 形如 https://idp.example.com/realms/eagle
	Issuer string
	// JWKSURL 为空时拼接为 <issuer><JWKSPath>。
	JWKSURL string
	// JWKSPath 是 JWKS 相对 issuer 的路径，留空取 DefaultJWKSPath。
	// JWKSURL 非空时本字段被忽略。
	JWKSPath string
	// ClientID 是本服务在 IdP 中的 client id，
	// 用于从 client 角色 claim 中取出本服务的角色
	ClientID string
	// Audience 非空时校验 aud 声明。
	// Keycloak 默认把 aud 设为 "account"，通常需要配 audience mapper 才有意义，
	// 因此默认不校验
	Audience string
	// Claims 指定角色在 token 载荷里的位置，留空取 Keycloak 约定。
	Claims ClaimPaths
}

// Verifier 验证 access token。
type Verifier struct {
	verifier *oidc.IDTokenVerifier
	clientID string
	claims   ClaimPaths
}

// NewVerifier 构造验证器。
//
// 这里刻意不做 OIDC discovery：discovery 会在构造时发起网络请求，
// 使得认证中心未就绪时资源服务器起不来。RemoteKeySet 是惰性的，
// 首个请求到达时才拉 JWKS，两个服务的启动顺序因此互不依赖。
func NewVerifier(ctx context.Context, cfg Config) *Verifier {
	jwksURL := cfg.JWKSURL
	if jwksURL == "" {
		path := cfg.JWKSPath
		if path == "" {
			path = DefaultJWKSPath
		}
		jwksURL = strings.TrimSuffix(cfg.Issuer, "/") + path
	}
	return &Verifier{
		verifier: oidc.NewVerifier(cfg.Issuer, oidc.NewRemoteKeySet(ctx, jwksURL), &oidc.Config{
			ClientID:             cfg.Audience,
			SkipClientIDCheck:    cfg.Audience == "",
			SupportedSigningAlgs: []string{"RS256"},
		}),
		clientID: cfg.ClientID,
		claims:   cfg.Claims,
	}
}

// Verify 校验 token 并返回其载荷。
func (v *Verifier) Verify(ctx context.Context, rawToken string) (*Claims, error) {
	token, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		var expired *oidc.TokenExpiredError
		if errors.As(err, &expired) {
			return nil, ErrTokenExpired
		}
		return nil, fmt.Errorf("%w: 签名校验失败: %w", ErrInvalidToken, err)
	}

	var claims Claims
	if err := token.Claims(&claims); err != nil {
		return nil, fmt.Errorf("%w: 载荷解析失败: %w", ErrInvalidToken, err)
	}

	return &claims, nil
}

// Server 返回认证中间件。
//
// 它不负责拒绝匿名请求——没有 token 时只是不放置 Principal，
// 由 authz 中间件按方法是否 public 来决定放行还是 401。
// 认证与授权的职责就此分开，public 接口不必绕过整条链路。
func Server(v *Verifier) middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			raw, ok := bearerToken(ctx)
			if !ok {
				return handler(ctx, req)
			}

			claims, err := v.Verify(ctx, raw)
			if err != nil {
				// token 存在但无效：直接拒绝，不能降级成匿名继续走。
				// 否则一个过期 token 会静默变成"未登录"，
				// 让本该 401 的请求在 public 接口上悄悄成功。
				return nil, toTransportError(err)
			}

			return handler(identity.NewContext(ctx, v.toPrincipal(claims)), req)
		}
	}
}

// toTransportError 把内部错误翻译成带状态码的传输层错误。
//
// 不做这层翻译的话，Kratos 会把无法识别的错误一律当成 500——
// 于是「token 过期」在客户端看来和「服务端崩了」没有区别：
// 前端不会去刷新 token，而监控里每个过期凭证都变成一次服务故障告警。
//
// 对外只给 reason，不透出内部错误文本：签名校验失败的具体原因
// 对调用方没有价值，对探测者反而是线索。
func toTransportError(err error) error {
	switch {
	case errors.Is(err, ErrTokenExpired):
		// 单独给一个 reason：客户端据此走刷新流程而不是把用户踢回登录页
		return kratoserrors.Unauthorized(ReasonTokenExpired, "登录已过期")
	default:
		return kratoserrors.Unauthorized(ReasonUnauthenticated, "凭证无效")
	}
}

func (v *Verifier) toPrincipal(c *Claims) *identity.Principal {
	return &identity.Principal{
		Subject:  c.Subject,
		Username: c.Username,
		Email:    c.Email,
		ClientID: c.AuthorizedParty,
		Scopes:   c.Scopes(),
		// realm 角色 + 本服务的 client 角色一并取出。
		Roles:       c.Roles(v.claims, v.clientID),
		ClientRoles: c.ClientRoles(v.claims, v.clientID),
	}
}

// bearerToken 从 Authorization 头取出 Bearer 令牌。
func bearerToken(ctx context.Context) (string, bool) {
	tr, ok := transport.FromServerContext(ctx)
	if !ok {
		return "", false
	}
	h := tr.RequestHeader().Get("Authorization")
	if h == "" {
		return "", false
	}

	const prefix = "Bearer "
	// 方案名大小写不敏感（RFC 7235），curl 之外的客户端未必按标准大小写发送
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(h[len(prefix):])
	return token, token != ""
}
