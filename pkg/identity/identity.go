// Package identity 用 context.Context 承载已认证主体。
// 认证中间件写入，业务与鉴权中间件只从这里读取。
package identity

import "context"

type ctxKey struct{}

// Principal 是一次调用的已认证主体，由认证中间件从 Keycloak token 构造。
//
// 它可能是终端用户，也可能是服务自身（client_credentials 服务账号），
// 由 IsService 区分。
type Principal struct {
	// Subject 是 Keycloak token 的 sub，用户在本系统的唯一锚点。
	//
	// 注意它是 UUID 字符串而不是自增整数：用户主数据在 Keycloak，
	// 本系统的 sys_user_profile 也以此为外部键关联。
	Subject string

	// Username 是 preferred_username。用于日志与审计，不参与鉴权判定。
	Username string
	Email    string

	// ClientID 是换取该 token 的客户端（Keycloak 的 azp）。
	ClientID string

	// Scopes 是 token 携带的 scope。
	Scopes []string

	// Roles 汇总了 realm 角色与本服务的 client 角色。
	// 普通 Casbin 策略判定基于它。
	Roles []string

	// ClientRoles 只包含本资源服务器在 resource_access 下的 client 角色。
	// 高影响的管理员短路必须基于它，防止 realm 级同名角色跨服务扩权。
	ClientRoles []string

	// TokenID 是 jti，登出/踢人时写入黑名单的键。
	TokenID string

	// IsService 标记这是服务账号令牌，背后没有真实用户。
	IsService bool
}

// HasRole 判断主体是否具备某个角色。
func (p *Principal) HasRole(role string) bool {
	if p == nil {
		return false
	}
	for _, r := range p.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// HasClientRole 判断主体是否拥有本资源服务器命名空间内的角色。
func (p *Principal) HasClientRole(role string) bool {
	if p == nil {
		return false
	}
	for _, r := range p.ClientRoles {
		if r == role {
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

// Subject 是取当前用户标识的便捷方法。
// 服务令牌或未认证时返回空串——服务账号不代表任何真实用户，
// 「查我自己的菜单」这类接口对它就不该有结果。
func Subject(ctx context.Context) string {
	p, ok := FromContext(ctx)
	if !ok || p.IsService {
		return ""
	}
	return p.Subject
}

// Roles 返回当前主体的角色，未认证时为 nil。
func Roles(ctx context.Context) []string {
	p, ok := FromContext(ctx)
	if !ok {
		return nil
	}
	return p.Roles
}
