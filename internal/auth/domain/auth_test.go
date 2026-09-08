package domain

import "testing"

func TestProviderValid(t *testing.T) {
	for _, tc := range []struct {
		provider Provider
		want     bool
	}{
		{ProviderGoogle, true},
		{ProviderApple, true},
		{"github", false},
		{"", false},
	} {
		if got := tc.provider.Valid(); got != tc.want {
			t.Errorf("Provider(%q).Valid() = %v, want %v", tc.provider, got, tc.want)
		}
	}
}
