package domain

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestNewRoleAcceptsOnlyPlainStableKeys(t *testing.T) {
	tests := []struct {
		value string
		valid bool
	}{
		{value: "user", valid: true},
		{value: "admin", valid: true},
		{value: "support-agent", valid: true},
		{value: "ops_2", valid: true},
		{value: "", valid: false},
		{value: "realm:admin", valid: false},
		{value: "2admin", valid: false},
		{value: "admin role", valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			role, err := NewRole(tt.value)
			if tt.valid && (err != nil || role.String() != tt.value) {
				t.Fatalf("NewRole(%q) = %q, %v", tt.value, role, err)
			}
			if !tt.valid && err == nil {
				t.Fatalf("NewRole(%q) unexpectedly succeeded", tt.value)
			}
		})
	}
}

func TestNewRoleRejectsKeysLongerThan64Bytes(t *testing.T) {
	value := "a1234567890123456789012345678901234567890123456789012345678901234"
	if _, err := NewRole(value); err == nil {
		t.Fatalf("NewRole accepted %d-byte key", len(value))
	}
}

func TestRoleBindingNormalizesCodesAndProtectsCopies(t *testing.T) {
	role, err := NewRole("editor")
	if err != nil {
		t.Fatal(err)
	}
	query := MustPermissionCode("system:user:list")
	add := MustPermissionCode("system:user:add")
	binding, err := NewRoleBinding(role, []PermissionCode{query, {}, add, query})
	if err != nil {
		t.Fatal(err)
	}
	if binding.Role() != role || binding.IsEmpty() {
		t.Fatalf("binding = %+v", binding)
	}
	if got := binding.CodeStrings(); !slices.Equal(got, []string{"system:user:add", "system:user:list"}) {
		t.Fatalf("codes = %v", got)
	}
	if !slices.Equal(binding.Codes(), []PermissionCode{add, query}) {
		t.Fatalf("typed codes = %v", binding.Codes())
	}

	withRevision := binding.WithRevision(12)
	if withRevision == binding || withRevision.Revision() != 12 || binding.Revision() != 0 {
		t.Fatalf("revisions = copied:%d original:%d", withRevision.Revision(), binding.Revision())
	}
	if (*RoleBinding)(nil).WithRevision(1) != nil {
		t.Fatal("nil binding should stay nil")
	}
	if _, err := NewRoleBinding("", nil); !errors.Is(err, ErrEmptyRole) {
		t.Fatalf("empty role error = %v", err)
	}
}

func TestRoleBindingGrantsAndValidatesCatalog(t *testing.T) {
	binding, err := NewRoleBinding(Role("ops"), []PermissionCode{
		MustPermissionCode("system:*"),
		MustPermissionCode("billing:invoice:list"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !binding.Grants(MustPermissionCode("system:user:add")) {
		t.Fatal("system wildcard should grant system permission")
	}
	if binding.Grants(MustPermissionCode("billing:invoice:add")) {
		t.Fatal("exact billing permission should not grant another action")
	}
	if err := binding.EnsureCodesKnown(map[string]struct{}{
		"billing:invoice:list": {},
	}); err != nil {
		t.Fatalf("known catalog rejected: %v", err)
	}
	if err := binding.EnsureCodesKnown(nil); !errors.Is(err, ErrUnknownPermissionCode) ||
		!strings.Contains(err.Error(), "billing:invoice:list") {
		t.Fatalf("unknown code error = %v", err)
	}
}

func TestRoleInheritanceInvariants(t *testing.T) {
	child, parent := Role("lead"), Role("editor")
	got, err := NewRoleInheritance(child, parent)
	if err != nil || got.Child != child || got.Parent != parent {
		t.Fatalf("inheritance = %+v, %v", got, err)
	}
	for _, tc := range []struct {
		child  Role
		parent Role
		want   error
	}{
		{"", parent, ErrEmptyRole},
		{child, "", ErrEmptyRole},
		{child, child, ErrSelfInheritance},
	} {
		if _, err := NewRoleInheritance(tc.child, tc.parent); !errors.Is(err, tc.want) {
			t.Fatalf("NewRoleInheritance(%q,%q) error = %v, want %v", tc.child, tc.parent, err, tc.want)
		}
	}
}
