package e2e

import (
	"encoding/json"
	"net/http"
	"testing"
)

type tokenResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

func TestSocialLoginReturnsUsableEagleToken(t *testing.T) {
	env := newTestEnv(t)
	tokens := env.socialLogin(t)

	code, body := env.get(t, "/v1/system/dict/data/type/sys_common_status", tokens.AccessToken)
	if code != http.StatusOK {
		t.Fatalf("authenticated API = %d (%s), want 200", code, body)
	}
}

func TestRefreshRotatesToken(t *testing.T) {
	env := newTestEnv(t)
	first := env.socialLogin(t)

	code, body := env.do(t, http.MethodPost, "/v1/auth/token/refresh", "",
		`{"refresh_token":"`+first.RefreshToken+`"}`)
	if code != http.StatusOK {
		t.Fatalf("refresh = %d (%s), want 200", code, body)
	}
	second := decodeTokenResponse(t, body)
	if second.RefreshToken == first.RefreshToken {
		t.Fatal("refresh token was not rotated")
	}

	code, _ = env.do(t, http.MethodPost, "/v1/auth/token/refresh", "",
		`{"refresh_token":"`+first.RefreshToken+`"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("old refresh token = %d, want 401", code)
	}
}

func TestLogoutRevokesRefreshToken(t *testing.T) {
	env := newTestEnv(t)
	tokens := env.socialLogin(t)

	code, body := env.do(t, http.MethodPost, "/v1/auth/logout", "",
		`{"refresh_token":"`+tokens.RefreshToken+`"}`)
	if code != http.StatusOK {
		t.Fatalf("logout = %d (%s), want 200", code, body)
	}

	code, _ = env.do(t, http.MethodPost, "/v1/auth/token/refresh", "",
		`{"refresh_token":"`+tokens.RefreshToken+`"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("revoked refresh token = %d, want 401", code)
	}
}

func (e *testEnv) socialLogin(t *testing.T) tokenResponse {
	t.Helper()
	code, body := e.do(t, http.MethodPost, "/v1/auth/social/login", "", `{
		"provider":"SOCIAL_PROVIDER_GOOGLE",
		"id_token":"valid-provider-token",
		"nonce":"valid-provider-nonce"
	}`)
	if code != http.StatusOK {
		t.Fatalf("social login = %d (%s), want 200", code, body)
	}
	return decodeTokenResponse(t, body)
}

func decodeTokenResponse(t *testing.T, body string) tokenResponse {
	t.Helper()
	var response tokenResponse
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		t.Fatalf("decode token response: %v (body=%s)", err, body)
	}
	if response.AccessToken == "" || response.RefreshToken == "" {
		t.Fatalf("missing tokens in response: %s", body)
	}
	return response
}
