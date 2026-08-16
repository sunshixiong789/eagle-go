package authz

import (
	"errors"
	"testing"

	"github.com/casbin/casbin/v2/model"
)

func TestEntAdapterRejectsCasbinWrites(t *testing.T) {
	a := NewEntAdapter(nil)
	m, err := model.NewModelFromString(ModelText)
	if err != nil {
		t.Fatalf("model: %v", err)
	}

	writes := []struct {
		name string
		err  error
	}{
		{"SavePolicy", a.SavePolicy(m)},
		{"AddPolicy", a.AddPolicy("", "p", []string{"r", "system:dict:list"})},
		{"AddPolicies", a.AddPolicies("", "p", [][]string{{"r", "system:dict:list"}})},
		{"RemovePolicy", a.RemovePolicy("", "p", []string{"r", "system:dict:list"})},
		{"RemovePolicies", a.RemovePolicies("", "p", [][]string{{"r", "system:dict:list"}})},
		{"RemoveFilteredPolicy", a.RemoveFilteredPolicy("", "p", 0, "r")},
	}
	for _, tc := range writes {
		if !errors.Is(tc.err, ErrAdapterReadOnly) {
			t.Errorf("%s = %v, want ErrAdapterReadOnly", tc.name, tc.err)
		}
	}
}
