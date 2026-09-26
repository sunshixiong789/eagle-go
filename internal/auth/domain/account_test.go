package domain

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestRoleAssignmentValidatesAndCopies(t *testing.T) {
	roles := []string{"support-agent", "admin", "admin"}
	got, err := NewRoleAssignment("subject", roles, 4, "operator")
	if err != nil || !slices.Equal(got.Roles, []string{"admin", "support-agent"}) {
		t.Fatalf("assignment=%+v error=%v", got, err)
	}
	roles[0] = "mutated"
	if got.Roles[1] != "support-agent" {
		t.Fatal("assignment aliases input")
	}
	for _, role := range []string{"", "1admin", "admin:*", "管理员", strings.Repeat("a", 65)} {
		if _, err := NewRoleAssignment("subject", []string{role}, 1, "actor"); !errors.Is(err, ErrInvalidRoleAssignment) {
			t.Fatalf("accepted role %q", role)
		}
	}
	for _, tc := range []struct {
		subject, actor string
		version        int64
		roles          []string
	}{
		{"", "actor", 1, nil}, {strings.Repeat("s", 33), "actor", 1, nil}, {"subject", "", 1, nil}, {"subject", strings.Repeat("a", 129), 1, nil}, {"subject", "actor", 0, nil}, {"subject", "actor", 1, make([]string, 33)},
	} {
		if _, err := NewRoleAssignment(tc.subject, tc.roles, tc.version, tc.actor); !errors.Is(err, ErrInvalidRoleAssignment) {
			t.Fatalf("accepted invalid assignment %+v", tc)
		}
	}
	if got, err := NewRoleAssignment("subject", nil, 1, "actor"); err != nil || len(got.Roles) != 0 {
		t.Fatal("empty role revocation rejected")
	}
}
