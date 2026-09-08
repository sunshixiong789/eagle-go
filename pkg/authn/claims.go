package authn

// Claims 是 Eagle access token 中业务请求实际消费的固定载荷。
type Claims struct {
	Subject   string   `json:"sub"`
	SessionID string   `json:"sid"`
	TokenID   string   `json:"jti"`
	Roles     []string `json:"roles"`
}
