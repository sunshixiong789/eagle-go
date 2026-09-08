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

type ProviderVerifier interface {
	Verify(context.Context, Provider, string, string) (*ExternalIdentity, error)
}

type SessionRepository interface {
	// Create 和 Rotate 必须在签发成功后提交会话；失败不消耗旧刷新凭证。
	Create(context.Context, *ExternalIdentity, string, Session) (*SessionGrant, error)
	Rotate(context.Context, string, string, time.Time) (*SessionGrant, error)
	Revoke(context.Context, string) error
}

type SessionGrant struct {
	Identity    *Identity
	AccessToken string
}

// AccessTokenIssuer 在本地签名，不执行网络 I/O；会话事务持有期间会调用它。
type AccessTokenIssuer interface {
	Issue(*Identity, string, time.Time) (string, error)
}
