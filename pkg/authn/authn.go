// Package authn verifies Eagle bearer access tokens and installs the current principal.
package authn

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
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

// Config 是 Eagle access token 的验证参数。
type Config struct {
	Issuer   string
	Audience string
	Keys     KeySource
}

// Verifier 验证 access token。
type Verifier struct {
	issuer   string
	audience string
	keys     KeySource
}

// NewVerifier 构造验证器。
func NewVerifier(cfg Config) (*Verifier, error) {
	if cfg.Issuer == "" || cfg.Audience == "" || cfg.Keys == nil {
		return nil, fmt.Errorf("authn: issuer、audience 和 key source 均为必填")
	}
	return &Verifier{issuer: cfg.Issuer, audience: cfg.Audience, keys: cfg.Keys}, nil
}

// Verify 校验 token 并返回其载荷。
func (v *Verifier) Verify(ctx context.Context, rawToken string) (*Claims, error) {
	return v.verify(ctx, rawToken)
}

func (v *Verifier) verify(ctx context.Context, rawToken string) (*Claims, error) {
	token, err := jwt.ParseSigned(rawToken, []jose.SignatureAlgorithm{jose.ES256, jose.RS256})
	if err != nil {
		return nil, fmt.Errorf("%w: token 格式无效", ErrInvalidToken)
	}
	if len(token.Headers) != 1 || token.Headers[0].KeyID == "" {
		return nil, fmt.Errorf("%w: token 缺少唯一 kid", ErrInvalidToken)
	}
	header := token.Headers[0]
	key, err := v.keys.Key(ctx, header.KeyID, header.Algorithm)
	if err != nil {
		return nil, fmt.Errorf("%w: 查找签名公钥: %w", ErrInvalidToken, err)
	}
	var standard jwt.Claims
	var claims Claims
	if err := token.Claims(key.Key, &standard, &claims); err != nil {
		return nil, fmt.Errorf("%w: 签名校验失败", ErrInvalidToken)
	}
	if err := standard.Validate(jwt.Expected{
		Issuer:      v.issuer,
		AnyAudience: jwt.Audience{v.audience},
		Time:        time.Now(),
	}); err != nil {
		if errors.Is(err, jwt.ErrExpired) {
			return nil, ErrTokenExpired
		}
		return nil, fmt.Errorf("%w: 标准声明校验失败", ErrInvalidToken)
	}
	if standard.Subject == "" || standard.ID == "" || standard.IssuedAt == nil || standard.Expiry == nil || claims.SessionID == "" {
		return nil, fmt.Errorf("%w: token 缺少必需声明", ErrInvalidToken)
	}
	claims.Subject = standard.Subject
	claims.TokenID = standard.ID
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
		Subject:   c.Subject,
		SessionID: c.SessionID,
		Roles:     slices.Clone(c.Roles),
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
