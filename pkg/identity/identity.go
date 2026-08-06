// Package identity 在 context 中承载已认证主体，
// 相当于 Spring Security 的 SecurityContextHolder，
// 只是这里用 context.Context 而非 ThreadLocal。
package identity

import "context"

type ctxKey struct{}

// Principal 是一次调用的已认证主体。
//
// 它可能是终端用户（授权码/后台直登换来的 token），
// 也可能是服务自身（client_credentials 换来的服务令牌）——由 IsService 区分。
type Principal struct {
	// Subject 是 token 的 sub。用户令牌为用户 ID 的十进制字符串，
	// 服务令牌为 client_id。
	Subject string

	// UserID 仅在用户令牌下有效（IsService 为 false）。
	UserID int64
	// Username 仅用于日志与审计，不参与鉴权判定。
	Username string

	// ClientID 是签发该 token 的 OAuth2 客户端。
	ClientID string
	// Scopes 是 token 携带的 scope。服务令牌靠它换取权限码。
	Scopes []string
	// RoleCodes 是用户所属角色码，用于超管短路。
	RoleCodes []string

	// TokenID 是 jti，登出/踢人时写入黑名单的键。
	TokenID string

	// IsService 标记这是 client_credentials 签发的服务令牌，
	// 背后没有真实用户，因此不做用户权限查询、只按 scope 判定。
	IsService bool
}

// HasRole 判断主体是否具备某个角色码。
func (p *Principal) HasRole(code string) bool {
	if p == nil {
		return false
	}
	for _, c := range p.RoleCodes {
		if c == code {
			return true
		}
	}
	return false
}

// HasScope 判断 token 是否携带某个 scope。
func (p *Principal) HasScope(scope string) bool {
	if p == nil {
		return false
	}
	for _, s := range p.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// NewContext 把主体放入 context。仅认证中间件应调用。
func NewContext(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// FromContext 取出主体。未认证时 ok 为 false。
func FromContext(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(*Principal)
	return p, ok && p != nil
}

// UserID 是取当前用户 ID 的便捷方法，服务令牌或未认证时返回 0。
// service 层处理 "me" 类接口（改自己密码、查自己菜单）时用它。
func UserID(ctx context.Context) int64 {
	p, ok := FromContext(ctx)
	if !ok || p.IsService {
		return 0
	}
	return p.UserID
}
