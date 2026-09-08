// Package domain contains provider-independent account identity and session concepts.
package domain

import (
	"context"
	"errors"
	"time"
)

var (
	ErrProviderDisabled    = errors.New("auth: 登录方式未启用")
	ErrInvalidIDToken      = errors.New("auth: 第三方身份凭证无效")
	ErrInvalidNonce        = errors.New("auth: 登录 nonce 无效")
	ErrInvalidRefreshToken = errors.New("auth: refresh token 无效或已过期")
	ErrAccountDisabled     = errors.New("auth: 账号已停用")
)

type Provider string

const (
	ProviderGoogle Provider = "google"
	ProviderApple  Provider = "apple"
)

func (p Provider) Valid() bool { return p == ProviderGoogle || p == ProviderApple }

type ExternalIdentity struct {
	Provider      Provider
	ProviderID    string
	Email         string
	EmailVerified bool
	DisplayName   string
	AvatarURL     string
}

type Identity struct {
	ID            int64
	Subject       string
	Provider      Provider
	ProviderID    string
	Email         string
	EmailVerified bool
	DisplayName   string
	AvatarURL     string
	Roles         []string
}

type Session struct {
	ID               string
	RefreshTokenHash string
	ExpiresAt        time.Time
}

type Tokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    time.Duration
	Identity     *Identity
}

// ProviderVerifier 验证第三方身份凭证，返回已验证的外部身份，不分配 Eagle 账号或角色。
type ProviderVerifier interface {
	// Verify 校验指定提供商的 ID Token 与原始 nonce；登录方式未启用、凭证无效、
	// nonce 不匹配时分别返回 ErrProviderDisabled、ErrInvalidIDToken、ErrInvalidNonce。
	Verify(context.Context, Provider, string, string) (*ExternalIdentity, error)
}

// SessionRepository 原子维护外部身份到 Eagle 账号的映射、账号角色和刷新会话。
// 会话仅保存刷新凭证的哈希；Create 和 Rotate 在本地签名成功后才提交事务。
type SessionRepository interface {
	// Create 为已验证的外部身份创建会话并签发令牌，已有身份复用原账号。
	// 新账号使用传入的候选 subject；账号停用返回 ErrAccountDisabled，签发失败回滚本次写入。
	Create(context.Context, *ExternalIdentity, string, Session) (*SessionGrant, error)
	// Rotate 用旧哈希原子换入新哈希及到期时间，并按当前账号和角色签发令牌。
	// 旧凭证无效、已使用、过期或已撤销返回 ErrInvalidRefreshToken；账号停用返回 ErrAccountDisabled。
	// 签发失败回滚轮换，不消耗旧凭证；同一旧凭证的并发轮换最多一次成功。
	Rotate(context.Context, string, string, time.Time) (*SessionGrant, error)
	// Revoke 按刷新凭证哈希撤销对应会话，不存在或已撤销返回 ErrInvalidRefreshToken。
	// 已签发的 access token 仍由资源服务独立验签，有效期不受此次撤销影响。
	Revoke(context.Context, string) error
}

type SessionGrant struct {
	Identity    *Identity
	AccessToken string
}

// AccessTokenIssuer 在本地签名，不执行网络 I/O；会话事务持有期间会调用它。
type AccessTokenIssuer interface {
	// Issue 为给定账号和会话签发 Eagle access token，以传入时间计算签发与到期时间。
	// 使用带 kid 的非对称签名，仅携带身份及授权必需声明，不写入用户资料。
	Issue(*Identity, string, time.Time) (string, error)
}
