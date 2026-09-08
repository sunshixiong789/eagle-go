package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/eagle-go/eagle/internal/auth/domain"
)

// Usecase 编排第三方身份验证、刷新凭证生成与会话操作。
type Usecase struct {
	providers  domain.ProviderVerifier
	sessions   domain.SessionRepository
	accessTTL  time.Duration
	refreshTTL time.Duration
	now        func() time.Time
}

func NewUsecase(providers domain.ProviderVerifier, sessions domain.SessionRepository, accessTTL, refreshTTL time.Duration) *Usecase {
	return &Usecase{providers: providers, sessions: sessions, accessTTL: accessTTL, refreshTTL: refreshTTL, now: time.Now}
}

// Login 验证第三方身份并生成账号候选 ID、会话和刷新凭证，再由仓储原子完成账号映射与签发。
// 第三方未提供显示名时使用客户端补充值；刷新凭证原文仅返回给调用方，仓储只接收哈希。
func (uc *Usecase) Login(ctx context.Context, provider domain.Provider, idToken, nonce, displayName string) (*domain.Tokens, error) {
	external, err := uc.providers.Verify(ctx, provider, idToken, nonce)
	if err != nil {
		return nil, err
	}
	if external.DisplayName == "" {
		external.DisplayName = displayName
	}
	refresh, hash, err := newRefreshToken()
	if err != nil {
		return nil, err
	}
	now := uc.now()
	accountSubject, err := randomID()
	if err != nil {
		return nil, err
	}
	sessionID, err := randomID()
	if err != nil {
		return nil, err
	}
	grant, err := uc.sessions.Create(ctx, external, accountSubject, domain.Session{ID: sessionID, RefreshTokenHash: hash, ExpiresAt: now.Add(uc.refreshTTL)})
	if err != nil {
		return nil, err
	}
	return &domain.Tokens{AccessToken: grant.AccessToken, RefreshToken: refresh, ExpiresIn: uc.accessTTL, Identity: grant.Identity}, nil
}

// Refresh 生成替换凭证，由仓储原子轮换并签发；仅在仓储成功后返回新凭证原文。
func (uc *Usecase) Refresh(ctx context.Context, refreshToken string) (*domain.Tokens, error) {
	refresh, newHash, err := newRefreshToken()
	if err != nil {
		return nil, err
	}
	now := uc.now()
	grant, err := uc.sessions.Rotate(ctx, tokenHash(refreshToken), newHash, now.Add(uc.refreshTTL))
	if err != nil {
		return nil, err
	}
	return &domain.Tokens{AccessToken: grant.AccessToken, RefreshToken: refresh, ExpiresIn: uc.accessTTL, Identity: grant.Identity}, nil
}

func (uc *Usecase) Logout(ctx context.Context, refreshToken string) error {
	return uc.sessions.Revoke(ctx, tokenHash(refreshToken))
}

func newRefreshToken() (string, string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate refresh token: %w", err)
	}
	raw := base64.RawURLEncoding.EncodeToString(b)
	return raw, tokenHash(raw), nil
}

func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func tokenHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
