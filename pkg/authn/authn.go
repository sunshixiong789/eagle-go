// Package authn 是 OAuth2 资源服务器侧的认证中间件。
//
// 它验证 access token 的签名（经认证中心的 JWKS）、时间与 issuer 声明，
// 再查一次撤销黑名单，最后把 identity.Principal 放进 context 交给 authz 判定授权。
//
// 签名验证在本地完成，不需要为每个请求回调认证中心；
// 只有撤销检查需要访问 Redis，且是一次 O(1) 的存在性查询。
package authn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"

	"github.com/eagle-go/eagle/pkg/identity"
)

// 认证相关错误。
var (
	ErrInvalidToken = errors.New("authn: token 无效")
	ErrTokenExpired = errors.New("authn: token 已过期")
	ErrTokenRevoked = errors.New("authn: token 已被撤销")
)

// Revocations 查询 token 是否已被撤销（登出、踢人、改密码）。
type Revocations interface {
	IsRevoked(ctx context.Context, jti string) (bool, error)
}

// Config 是资源服务器的验证参数。
type Config struct {
	// Issuer 必须与认证中心 discovery 文档里的 issuer 完全一致
	Issuer string
	// JWKSURL 为空时按 OIDC 约定推导为 <issuer>/.well-known/jwks.json
	JWKSURL string
	// Audience 非空时校验 aud 声明
	Audience string
	// Leeway 容忍的时钟漂移，默认 30s
	Leeway time.Duration
}

// Verifier 验证 access token。
type Verifier struct {
	keySet      oidc.KeySet
	issuer      string
	audience    string
	leeway      time.Duration
	revocations Revocations
}

// NewVerifier 构造验证器。
//
// 这里刻意不做 OIDC discovery：discovery 会在构造时发起网络请求，
// 使得认证中心未就绪时资源服务器起不来。RemoteKeySet 是惰性的，
// 首个请求到达时才拉 JWKS，两个服务的启动顺序因此互不依赖。
func NewVerifier(ctx context.Context, cfg Config, revocations Revocations) *Verifier {
	jwksURL := cfg.JWKSURL
	if jwksURL == "" {
		jwksURL = strings.TrimSuffix(cfg.Issuer, "/") + "/.well-known/jwks.json"
	}
	leeway := cfg.Leeway
	if leeway <= 0 {
		leeway = 30 * time.Second
	}

	return &Verifier{
		keySet:      oidc.NewRemoteKeySet(ctx, jwksURL),
		issuer:      cfg.Issuer,
		audience:    cfg.Audience,
		leeway:      leeway,
		revocations: revocations,
	}
}

// Verify 校验 token 并返回其载荷。
func (v *Verifier) Verify(ctx context.Context, rawToken string) (*Claims, error) {
	// RemoteKeySet 按 kid 选公钥，并在遇到未知 kid 时自动重拉 JWKS，
	// 因此认证中心轮转密钥后无需重启资源服务器
	payload, err := v.keySet.VerifySignature(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("%w: 签名校验失败: %w", ErrInvalidToken, err)
	}

	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("%w: 载荷解析失败: %w", ErrInvalidToken, err)
	}

	if err := claims.validate(v.issuer, time.Now(), v.leeway); err != nil {
		return nil, err
	}
	if v.audience != "" && !claims.Audience.contains(v.audience) {
		return nil, fmt.Errorf("%w: aud 不匹配", ErrInvalidToken)
	}

	// 撤销检查放在最后：只有本来就合法的 token 才值得查一次 Redis
	if v.revocations != nil && claims.JTI != "" {
		revoked, err := v.revocations.IsRevoked(ctx, claims.JTI)
		if err != nil {
			// 查不到撤销状态时按"已撤销"处理。
			// 放行会让 Redis 故障直接变成"所有已登出的 token 重新可用"。
			return nil, fmt.Errorf("%w: 撤销状态不可知", ErrTokenRevoked)
		}
		if revoked {
			return nil, ErrTokenRevoked
		}
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
				return nil, err
			}

			return handler(identity.NewContext(ctx, toPrincipal(claims)), req)
		}
	}
}

func toPrincipal(c *Claims) *identity.Principal {
	p := &identity.Principal{
		Subject:   c.Subject,
		ClientID:  c.ClientID,
		Scopes:    c.Scopes(),
		TokenID:   c.JTI,
		IsService: c.IsServiceToken(),
	}
	if !p.IsService {
		p.UserID = c.UserID()
		p.Username = c.Username
		p.RoleCodes = c.Roles
	}
	return p
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
