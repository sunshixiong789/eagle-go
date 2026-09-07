// Package identity 用 context.Context 承载已认证主体。
// 认证中间件写入，业务与鉴权中间件只从这里读取。
package identity

import (
	"context"
)

type ctxKey struct{}

// Principal 是一次调用的已认证主体，由认证中间件从 Eagle token 构造。
type Principal struct {
	// Subject 是 token 的 sub，用户在本系统的唯一锚点。
	Subject string

	// Username 用于展示、日志与审计，不参与鉴权判定。
	Username string
	Email    string

	// Roles 是 Eagle 分配的稳定角色键，Casbin 判定基于它。
	Roles []string
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

// Subject returns the authenticated subject or an empty string when the
// context is unauthenticated. Protected handlers normally receive a value.
func Subject(ctx context.Context) string {
	p, ok := FromContext(ctx)
	if !ok {
		return ""
	}
	return p.Subject
}
