package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

var socialLoginSeq atomic.Uint64

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
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

func TestLogoutRevokesAccessTokenImmediately(t *testing.T) {
	env := newTestEnv(t)
	tokens := env.socialLogin(t)
	code, body := env.do(t, http.MethodPost, "/v1/auth/logout", "",
		`{"refresh_token":"`+tokens.RefreshToken+`"}`)
	if code != http.StatusOK {
		t.Fatalf("logout = %d (%s), want 200", code, body)
	}
	code, body = env.get(t, "/v1/auth/sessions", tokens.AccessToken)
	if code != http.StatusUnauthorized {
		t.Fatalf("revoked access token = %d (%s), want 401", code, body)
	}
}

func TestLoginCredentialCannotBeReplayed(t *testing.T) {
	env := newTestEnv(t)
	const idToken = "valid-provider-token-replay"
	env.socialLoginWithToken(t, idToken)
	code, body := env.do(t, http.MethodPost, "/v1/auth/social/login", "", `{
		"provider":1,
		"id_token":"`+idToken+`",
		"nonce":"valid-provider-nonce"
	}`)
	if code != http.StatusUnauthorized || !strings.Contains(body, "ERROR_REASON_CREDENTIAL_USED") {
		t.Fatalf("replay = %d (%s), want 401 CREDENTIAL_USED", code, body)
	}
}

func TestListAndRevokeOtherSession(t *testing.T) {
	env := newTestEnv(t)
	first := env.socialLoginWithToken(t, "valid-provider-token-same-user-1")
	second := env.socialLoginWithToken(t, "valid-provider-token-same-user-2")
	code, body := env.get(t, "/v1/auth/sessions", first.AccessToken)
	if code != http.StatusOK {
		t.Fatalf("list sessions = %d (%s)", code, body)
	}
	var listed struct {
		Sessions []struct {
			SessionID string `json:"session_id"`
			Current   bool   `json:"current"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(body), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Sessions) != 2 {
		t.Fatalf("sessions = %+v", listed.Sessions)
	}
	var other string
	for _, session := range listed.Sessions {
		if session.Current {
			code, body = env.do(t, http.MethodPost, "/v1/auth/sessions/"+session.SessionID+"/revoke", first.AccessToken, "{}")
			if code != http.StatusBadRequest || !strings.Contains(body, "ERROR_REASON_CANNOT_REVOKE_CURRENT") {
				t.Fatalf("revoke current = %d (%s), want 400", code, body)
			}
			continue
		}
		other = session.SessionID
	}
	code, body = env.do(t, http.MethodPost, "/v1/auth/sessions/"+other+"/revoke", first.AccessToken, "{}")
	if code != http.StatusOK {
		t.Fatalf("revoke other = %d (%s)", code, body)
	}
	code, body = env.get(t, "/v1/auth/sessions", second.AccessToken)
	if code != http.StatusUnauthorized {
		t.Fatalf("revoked session access = %d (%s), want 401", code, body)
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
	return e.socialLoginWithToken(t, fmt.Sprintf("valid-provider-token-%d", socialLoginSeq.Add(1)))
}

func (e *testEnv) socialLoginWithToken(t *testing.T, idToken string) tokenResponse {
	t.Helper()
	code, body := e.do(t, http.MethodPost, "/v1/auth/social/login", "", `{
		"provider":1,
		"id_token":"`+idToken+`",
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
