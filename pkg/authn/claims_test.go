package authn

import (
	"encoding/json"
	"slices"
	"testing"
)

func mustClaims(t *testing.T, payload string) *Claims {
	t.Helper()
	var c Claims
	if err := json.Unmarshal([]byte(payload), &c); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	return &c
}

func TestClaimsSeparatesClientRolesFromRealmRoles(t *testing.T) {
	claims := mustClaims(t, `{
		"sub": "u-1",
		"realm_access": {"roles": ["admin", "realm-viewer"]},
		"resource_access": {
			"eagle-system": {"roles": ["system-admin", "editor"]},
			"other": {"roles": ["foreign-admin"]}
		}
	}`)

	if got := claims.ClientRoles(ClaimPaths{}, "eagle-system"); !slices.Equal(got, []string{"system-admin", "editor"}) {
		t.Fatalf("ClientRoles = %v", got)
	}
	wantAll := []string{
		"realm:admin",
		"realm:realm-viewer",
		"client:eagle-system:system-admin",
		"client:eagle-system:editor",
	}
	if got := claims.Roles(ClaimPaths{}, "eagle-system"); !slices.Equal(got, wantAll) {
		t.Fatalf("Roles = %v, want %v", got, wantAll)
	}
}

// 换 IdP 只改配置：同一份代码要能读出非 Keycloak 布局的角色。
func TestClaimsHonoursConfiguredClaimPaths(t *testing.T) {
	claims := mustClaims(t, `{
		"sub": "u-1",
		"roles": ["tenant-admin"],
		"https://eagle.example.com/roles": ["system-admin"]
	}`)
	paths := ClaimPaths{
		RealmRoles:  "roles",
		ClientRoles: "https://eagle.example.com/roles",
	}

	// claim 名本身含点号（URL 风格），不能被按点拆开。
	if got := claims.ClientRoles(paths, "eagle-system"); !slices.Equal(got, []string{"system-admin"}) {
		t.Fatalf("ClientRoles = %v", got)
	}
	want := []string{"realm:tenant-admin", "client:eagle-system:system-admin"}
	if got := claims.Roles(paths, "eagle-system"); !slices.Equal(got, want) {
		t.Fatalf("Roles = %v, want %v", got, want)
	}
}

func TestClaimsMissingRoleClaimsYieldNoRoles(t *testing.T) {
	claims := mustClaims(t, `{"sub": "u-1"}`)

	if got := claims.Roles(ClaimPaths{}, "eagle-system"); len(got) != 0 {
		t.Fatalf("Roles = %v, want empty", got)
	}
	if got := claims.ClientRoles(ClaimPaths{}, "eagle-system"); len(got) != 0 {
		t.Fatalf("ClientRoles = %v, want empty", got)
	}
}

func TestClaimsScopes(t *testing.T) {
	claims := mustClaims(t, `{"sub": "u-1", "scope": "openid profile"}`)
	if got := claims.Scopes(); !slices.Equal(got, []string{"openid", "profile"}) {
		t.Fatalf("Scopes = %v", got)
	}
}
