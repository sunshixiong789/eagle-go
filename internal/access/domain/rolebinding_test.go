package domain

import "testing"

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
