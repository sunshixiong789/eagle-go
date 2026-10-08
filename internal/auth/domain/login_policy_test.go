package domain

import (
	"slices"
	"testing"
)

func TestMergeLoginProfile(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		current, incoming, want LoginProfile
	}{
		{"首次补全", LoginProfile{}, LoginProfile{"Alice", "new"}, LoginProfile{"Alice", "new"}},
		{"保留昵称更新头像", LoginProfile{"Local", "old"}, LoginProfile{"Remote", "new"}, LoginProfile{"Local", "new"}},
		{"空资料不清除已有值", LoginProfile{"Local", "old"}, LoginProfile{}, LoginProfile{"Local", "old"}},
		{"只补昵称", LoginProfile{"", "old"}, LoginProfile{"Alice", ""}, LoginProfile{"Alice", "old"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := MergeLoginProfile(tc.current, tc.incoming); got != tc.want {
				t.Fatalf("profile = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestInitialAudienceRoles(t *testing.T) {
	if got := InitialAudienceRoles(nil); !slices.Equal(got, []string{"user"}) {
		t.Fatalf("initial roles = %v", got)
	}
	existing := []string{"support", "admin"}
	got := InitialAudienceRoles(existing)
	if !slices.Equal(got, existing) {
		t.Fatalf("existing roles changed: %v", got)
	}
	got[0] = "user"
	if existing[0] != "support" {
		t.Fatal("returned roles alias caller's slice")
	}
}
