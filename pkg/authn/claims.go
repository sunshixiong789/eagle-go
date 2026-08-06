package authn

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// serviceAccountPrefix 是 Keycloak 为 client_credentials 服务账号
// 生成的用户名前缀，形如 service-account-eagle-system。
// 这是 Keycloak 的既定约定，也是区分「服务令牌」与「用户令牌」最可靠的信号。
const serviceAccountPrefix = "service-account-"

// Claims 是 Keycloak 签发的 access token 载荷。
//
// 只声明本项目实际消费的字段。Keycloak 的 token 还带 session_state、
// allowed-origins、email_verified 等一堆字段，全声明进来会造成
// 「这些都参与判定」的错觉——鉴权结构体尤其要避免这种误导。
type Claims struct {
	Issuer    string   `json:"iss"`
	Subject   string   `json:"sub"`
	Audience  audience `json:"aud"`
	ExpiresAt int64    `json:"exp"`
	IssuedAt  int64    `json:"iat"`
	// JTI 是 token 唯一标识，登出/踢人时写进黑名单
	JTI string `json:"jti"`

	// AuthorizedParty 是 Keycloak 的 azp，即换取该 token 的客户端。
	// 注意不是 client_id——Keycloak 的 access token 里用的是 azp。
	AuthorizedParty string `json:"azp"`

	// Scope 为空格分隔，遵循 RFC 6749
	Scope string `json:"scope"`

	Username string `json:"preferred_username"`
	Email    string `json:"email"`

	// RealmAccess 是 realm 级角色。Keycloak 把角色嵌在这里，
	// 而不是放在顶层 roles claim——直接读 roles 会永远拿到空。
	RealmAccess struct {
		Roles []string `json:"roles"`
	} `json:"realm_access"`

	// ResourceAccess 是各客户端的 client 级角色，键为 client id。
	ResourceAccess map[string]struct {
		Roles []string `json:"roles"`
	} `json:"resource_access"`
}

// Scopes 把空格分隔的 scope 串拆成切片。
func (c *Claims) Scopes() []string {
	if c.Scope == "" {
		return nil
	}
	return strings.Fields(c.Scope)
}

// Roles 汇总 realm 角色与指定客户端的 client 角色。
//
// clientID 为空时只返回 realm 角色。Casbin 判定和超管短路都基于这个结果，
// 所以两级角色必须一起返回——只看其中一级会让在 Keycloak 里
// 按 client 授权的角色静默失效。
func (c *Claims) Roles(clientID string) []string {
	roles := make([]string, 0, len(c.RealmAccess.Roles))
	roles = append(roles, c.RealmAccess.Roles...)

	if clientID != "" {
		if ra, ok := c.ResourceAccess[clientID]; ok {
			roles = append(roles, ra.Roles...)
		}
	}
	return roles
}

// IsServiceToken 判断这是不是 client_credentials 签发的服务令牌。
//
// Keycloak 下不能用「sub == client_id」判断：服务账号有自己的用户 UUID，
// sub 是那个 UUID 而不是客户端标识。可靠信号是用户名的
// service-account- 前缀。
func (c *Claims) IsServiceToken() bool {
	return strings.HasPrefix(c.Username, serviceAccountPrefix)
}

// ServiceName 从服务账号用户名里还原出客户端标识。
// 非服务令牌返回空串。
func (c *Claims) ServiceName() string {
	if !c.IsServiceToken() {
		return ""
	}
	if c.AuthorizedParty != "" {
		return c.AuthorizedParty
	}
	return strings.TrimPrefix(c.Username, serviceAccountPrefix)
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
	if c.IssuedAt != 0 && now.Add(leeway).Before(time.Unix(c.IssuedAt, 0)) {
		return fmt.Errorf("%w: iat 位于未来", ErrInvalidToken)
	}
	return nil
}

// audience 兼容 aud 的两种合法形式：字符串或字符串数组。
// Keycloak 单个受众时发字符串、多个时发数组，只处理一种会随配置变化而失败。
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
