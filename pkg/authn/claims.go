package authn

import (
	"encoding/json"
	"strings"

	"github.com/eagle-go/eagle/pkg/identity"
)

// ClaimPaths 指定角色在 token 载荷中的位置，点号分隔逐层下钻。
type ClaimPaths struct {
	// RealmRoles 指向一个字符串数组，是不区分客户端的全局角色。
	RealmRoles string
	// ClientRoles 指向 {"<client-id>": {"roles": [...]}} 结构；
	// 若该路径直接是一个字符串数组，则整体视为本 client 的角色。
	ClientRoles string
}

func (p ClaimPaths) realmRoles() string {
	return p.RealmRoles
}

func (p ClaimPaths) clientRoles() string {
	return p.ClientRoles
}

// Claims 是 IdP 签发的 access token 载荷。
//
// 标准字段用结构体接：sub / azp / scope / preferred_username / email
// 在各家 IdP 之间是一致的。角色不走结构体而是从 raw 里按配置路径取，
// 原因见 ClaimPaths。
//
// 只声明本项目实际消费的字段。token 里还带 session_state、
// allowed-origins、email_verified 等一堆内容，全声明进来会造成
// 「这些都参与判定」的错觉——鉴权结构体尤其要避免这种误导。
type Claims struct {
	Subject string `json:"sub"`
	// AuthorizedParty 是 azp，即换取该 token 的客户端。
	// 注意不是 client_id——access token 里用的是 azp。
	AuthorizedParty string `json:"azp"`

	// Scope 为空格分隔，遵循 RFC 6749
	Scope string `json:"scope"`

	Username string `json:"preferred_username"`
	Email    string `json:"email"`

	// raw 是完整载荷，供按路径取角色使用。
	raw map[string]any
}

// UnmarshalJSON 在填充结构化字段的同时保留原始载荷。
func (c *Claims) UnmarshalJSON(data []byte) error {
	// 定义新类型剥掉方法集，否则 Unmarshal 会递归调用本方法。
	type plain Claims
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*c = Claims(p)
	return json.Unmarshal(data, &c.raw)
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
// 所以两级角色必须一起返回——只看其中一级会让在 IdP 里
// 按 client 授权的角色静默失效。
func (c *Claims) Roles(paths ClaimPaths, clientID string) []string {
	realm := stringSlice(lookupClaim(c.raw, paths.realmRoles()))
	client := c.ClientRoles(paths, clientID)

	roles := make([]string, 0, len(realm)+len(client))
	for _, role := range realm {
		roles = append(roles, identity.RealmRoleKey(role))
	}
	for _, role := range client {
		roles = append(roles, identity.ClientRoleKey(clientID, role))
	}
	return roles
}

// ClientRoles 只返回指定资源服务器命名空间内的角色。
// 超级管理员之类的高影响短路只能基于这份集合，避免同名 realm 角色
// 意外获得所有服务的全局管理能力。
func (c *Claims) ClientRoles(paths ClaimPaths, clientID string) []string {
	if clientID == "" {
		return nil
	}

	node := lookupClaim(c.raw, paths.clientRoles())
	// 路径直接指向数组：该 claim 整体就是本 client 的角色列表。
	// Auth0 这类把角色平铺进单个自定义 claim 的 IdP 走这条分支，
	// 否则它们的角色永远进不了 ClientRoles，超管短路也就永远不触发。
	if roles := stringSlice(node); len(roles) > 0 {
		return roles
	}

	group, ok := node.(map[string]any)
	if !ok {
		return nil
	}
	entry, ok := group[clientID].(map[string]any)
	if !ok {
		return nil
	}
	return stringSlice(entry["roles"])
}

// lookupClaim 按点号分隔的路径在载荷里逐层下钻。
//
// 先整体当作一个 key 命中再逐层拆分：Auth0 风格的 claim 名本身就是 URL
// （https://eagle.example.com/roles），里面带点，按点拆会直接找不到。
func lookupClaim(raw map[string]any, path string) any {
	if raw == nil || path == "" {
		return nil
	}
	if v, ok := raw[path]; ok {
		return v
	}

	var cur any = raw
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = m[seg]
		if !ok {
			return nil
		}
	}
	return cur
}

// stringSlice 把 JSON 数组转成字符串切片，忽略非字符串与空串元素。
func stringSlice(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}
