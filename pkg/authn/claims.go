package authn

// Claims 是 Eagle access token 中业务请求实际消费的固定载荷。
type Claims struct {
	Subject  string   `json:"sub"`
	Username string   `json:"preferred_username"`
	Email    string   `json:"email"`
	Roles    []string `json:"roles"`
}
