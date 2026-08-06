// Package authz 提供方法级鉴权中间件，对标 Spring Security 的 @PreAuthorize。
//
// 权限要求声明在 proto 上（eagle.annotations.v1.perm），中间件在运行时
// 从方法描述符读出并判定，业务 handler 里不出现任何鉴权代码。
//
// 判定顺序：
//  1. public 方法直接放行
//  2. 无主体 -> 401
//  3. 未声明权限码 -> 已登录即可
//  4. 服务令牌 -> 按 scope 判定
//  5. 超管角色 -> 短路放行
//  6. 其余 -> 查用户权限码集合
package authz

import (
	"context"

	"github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"

	"github.com/eagle-go/eagle/pkg/identity"
)

// 错误 reason，客户端据此做分支处理。
const (
	ReasonUnauthenticated = "UNAUTHENTICATED"
	ReasonForbidden       = "FORBIDDEN"
)

// PermissionLoader 返回用户拥有的全部权限码。
// 实现方应带缓存——它在每个需要鉴权的请求上都会被调用。
type PermissionLoader func(ctx context.Context, userID int64) ([]string, error)

type options struct {
	loader         PermissionLoader
	superAdminRole string
}

// Option 配置鉴权中间件。
type Option func(*options)

// WithPermissionLoader 注入权限码来源。不设置时，
// 所有声明了权限码的方法都会被拒绝（fail closed）。
func WithPermissionLoader(l PermissionLoader) Option {
	return func(o *options) { o.loader = l }
}

// WithSuperAdminRole 设置超管角色码，具备该角色的用户跳过权限判定。
func WithSuperAdminRole(code string) Option {
	return func(o *options) { o.superAdminRole = code }
}

// Server 返回鉴权中间件。
//
// 它必须挂在认证中间件之后——后者负责验签 token 并把
// identity.Principal 放进 context，这里只做授权判定。
func Server(opts ...Option) middleware.Middleware {
	o := &options{superAdminRole: "admin"}
	for _, opt := range opts {
		opt(o)
	}

	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			tr, ok := transport.FromServerContext(ctx)
			if !ok {
				// 拿不到传输层信息说明中间件被挂在了非服务端链路上，
				// 此时无法判定，按拒绝处理
				return nil, errors.Forbidden(ReasonForbidden, "无法解析调用上下文")
			}

			policy := PolicyFor(tr.Operation())
			if policy.Public {
				return handler(ctx, req)
			}

			p, authenticated := identity.FromContext(ctx)
			if !authenticated {
				return nil, errors.Unauthorized(ReasonUnauthenticated, "未登录或凭证已失效")
			}

			// 未声明权限码：登录即可访问（如查自己的菜单、改自己的密码）
			if policy.Perm == "" {
				return handler(ctx, req)
			}

			if err := o.check(ctx, p, policy.Perm); err != nil {
				return nil, err
			}
			return handler(ctx, req)
		}
	}
}

func (o *options) check(ctx context.Context, p *identity.Principal, perm string) error {
	// 服务令牌背后没有用户，权限直接来自 token 的 scope。
	// 服务间调用不应该、也无法查"用户权限表"。
	if p.IsService {
		if p.HasScope(perm) {
			return nil
		}
		return errors.Forbidden(ReasonForbidden, "服务令牌缺少所需 scope: "+perm)
	}

	if o.superAdminRole != "" && p.HasRole(o.superAdminRole) {
		return nil
	}

	if o.loader == nil {
		// 没配 loader 却要求权限码，属于装配错误。
		// 这里选择拒绝而不是放行——鉴权组件的失败方向必须是关闭的。
		return errors.Forbidden(ReasonForbidden, "鉴权未正确装配：缺少权限加载器")
	}

	codes, err := o.loader(ctx, p.UserID)
	if err != nil {
		// 查不到权限时同样拒绝。放行会让数据库抖动直接变成越权。
		return errors.Forbidden(ReasonForbidden, "权限校验失败")
	}

	for _, c := range codes {
		if c == perm {
			return nil
		}
	}
	return errors.Forbidden(ReasonForbidden, "缺少权限: "+perm)
}
