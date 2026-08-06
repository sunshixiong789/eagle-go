package authn

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Claims 是 eagle 签发的 access token 载荷。
//
// 只声明本项目实际消费的字段。多余的声明会造成一种"这些字段都参与判定"
// 的错觉，实际却没人读——鉴权相关的结构体尤其要避免这种误导。
type Claims struct {
	Issuer    string   `json:"iss"`
	Subject   string   `json:"sub"`
	Audience  audience `json:"aud"`
	ExpiresAt int64    `json:"exp"`
	IssuedAt  int64    `json:"iat"`
	// JTI 是 token 唯一标识，登出/踢人时写进黑名单
	JTI string `json:"jti"`

	// ClientID 是签发该 token 的 OAuth2 客户端
	ClientID string `json:"client_id"`
	// Scope 为空格分隔，遵循 RFC 6749
	Scope string `json:"scope"`

	// 以下字段只在用户令牌上出现，client_credentials 令牌没有
	Username string   `json:"preferred_username"`
	Roles    []string `json:"roles"`
}

// Scopes 把空格分隔的 scope 串拆成切片。
func (c *Claims) Scopes() []string {
	if c.Scope == "" {
		return nil
	}
	return strings.Fields(c.Scope)
}

// UserID 从 sub 解析用户 ID。
// 服务令牌的 sub 是 client_id，解析不出数字，返回 0。
func (c *Claims) UserID() int64 {
	id, err := strconv.ParseInt(c.Subject, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// IsServiceToken 判断这是不是 client_credentials 签发的服务令牌。
// 判据是 sub 等于 client_id——该 grant 背后没有用户，
// 规范做法就是把 sub 设成客户端标识。
func (c *Claims) IsServiceToken() bool {
	return c.ClientID != "" && c.Subject == c.ClientID
}

// validate 校验与签名无关的时间和 issuer 声明。
// 签名由 JWKS 负责，这里补上其余部分。
func (c *Claims) validate(wantIssuer string, now time.Time, leeway time.Duration) error {
	if c.Issuer != wantIssuer {
		return fmt.Errorf("%w: issuer %q, want %q", ErrInvalidToken, c.Issuer, wantIssuer)
	}
	if c.ExpiresAt == 0 {
		return fmt.Errorf("%w: 缺少 exp 声明", ErrInvalidToken)
	}
	if now.After(time.Unix(c.ExpiresAt, 0).Add(leeway)) {
		return ErrTokenExpired
	}
	// iat 允许缺省；存在时不接受明显来自未来的 token（时钟漂移之外）
	if c.IssuedAt != 0 && now.Add(leeway).Before(time.Unix(c.IssuedAt, 0)) {
		return fmt.Errorf("%w: iat 位于未来", ErrInvalidToken)
	}
	return nil
}

// audience 兼容 aud 的两种合法形式：字符串或字符串数组。
// JWT 规范两者都允许，只处理其中一种会在对接其他 IdP 时踩坑。
type audience []string

func (a *audience) UnmarshalJSON(b []byte) error {
	var single string
	if err := json.Unmarshal(b, &single); err == nil {
		*a = audience{single}
		return nil
	}
	var multi []string
	if err := json.Unmarshal(b, &multi); err != nil {
		return fmt.Errorf("aud 既不是字符串也不是字符串数组: %w", err)
	}
	*a = multi
	return nil
}

func (a audience) contains(v string) bool {
	for _, s := range a {
		if s == v {
			return true
		}
	}
	return false
}
