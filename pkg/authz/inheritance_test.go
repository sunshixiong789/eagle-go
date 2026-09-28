package authz

import (
	"context"
	"fmt"
	"slices"
	"testing"
)

func inheritanceRows(depth int) []StoredPolicy {
	rows := []StoredPolicy{{PType: "p", Values: []string{fmt.Sprintf("role%d", depth), "system:dict:list"}}}
	for i := 0; i < depth; i++ {
		rows = append(rows, StoredPolicy{PType: "g", Values: []string{fmt.Sprintf("role%d", i), fmt.Sprintf("role%d", i+1)}})
	}
	return rows
}

// TestDeepInheritanceSurvivesReload 覆盖默认边界、深链和重载后链增长，并验证删除继承后权限撤销。
func TestDeepInheritanceSurvivesReload(t *testing.T) {
	source := &mutablePolicySource{rows: inheritanceRows(10), version: 1}
	en, err := NewEnforcer(NewStorageAdapter(source))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, depth := range []int{10, 11, 32} {
		source.rows = inheritanceRows(depth)
		source.version++
		if err := en.ReloadPolicy(ctx); err != nil {
			t.Fatal(err)
		}
		if en.LoadedPolicyVersion() != source.version {
			t.Fatal("loaded version mismatch")
		}
		allowed, err := en.Allow([]string{"role0"}, "system:dict:list")
		if err != nil || !allowed {
			t.Fatalf("depth=%d allowed=%v error=%v", depth, allowed, err)
		}
		codes, err := en.PermissionsOf(ctx, []string{"role0"})
		if err != nil || !slices.Equal(codes, []string{"system:dict:list"}) {
			t.Fatalf("depth=%d codes=%v error=%v", depth, codes, err)
		}
		if allowed, err := en.Allow([]string{"outsider"}, "system:dict:list"); err != nil || allowed {
			t.Fatalf("unrelated role allowed=%v error=%v", allowed, err)
		}
	}
	source.rows = source.rows[:1]
	source.version++
	if err := en.ReloadPolicy(ctx); err != nil {
		t.Fatal(err)
	}
	if allowed, err := en.Allow([]string{"role0"}, "system:dict:list"); err != nil || allowed {
		t.Fatalf("removed inheritance allowed=%v error=%v", allowed, err)
	}
	codes, err := en.PermissionsOf(ctx, []string{"role0"})
	if err != nil || len(codes) != 0 {
		t.Fatalf("removed inheritance codes=%v error=%v", codes, err)
	}
}
