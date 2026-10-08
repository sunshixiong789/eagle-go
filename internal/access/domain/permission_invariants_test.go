package domain

import (
	"errors"
	"testing"
)

func TestPermissionStatusInvariant(t *testing.T) {
	for _, status := range []int32{-1, 2} {
		params := validParams()
		params.Status = status
		if _, err := NewPermission(params); !errors.Is(err, ErrInvalidPermissionStatus) {
			t.Fatalf("create status %d: %v", status, err)
		}
		p, err := NewPermission(validParams())
		if err != nil {
			t.Fatal(err)
		}
		before := *p
		if err := p.Update(params); !errors.Is(err, ErrInvalidPermissionStatus) {
			t.Fatalf("update status %d: %v", status, err)
		}
		if *p != before {
			t.Fatal("failed update changed entity")
		}
		if _, err := RehydratePermission(snap(1, 0, "menu", "", PermissionTypeMenu, status, 0)); !errors.Is(err, ErrInvalidPermissionStatus) {
			t.Fatalf("rehydrate status %d: %v", status, err)
		}
	}
	params := validParams()
	params.Status = 0
	if _, err := NewPermission(params); err != nil {
		t.Fatalf("disabled status: %v", err)
	}
}

func TestRehydratePermissionValidatesCode(t *testing.T) {
	for _, code := range []string{"bad-code", "system:*", "system:user:bad-name"} {
		if _, err := RehydratePermission(snap(1, 0, "menu", code, PermissionTypeMenu, 1, 0)); !errors.Is(err, ErrInvalidPermissionCode) {
			t.Fatalf("rehydrate %q: %v", code, err)
		}
	}
}

func TestPermissionTreeDeepAncestorsAndExistingCycle(t *testing.T) {
	var nodes []*Permission
	for id := int64(1); id <= 100; id++ {
		nodes = append(nodes, mustRehydratePermission(snap(id, id-1, "dir", "", PermissionTypeDir, 1, 0)))
	}
	tree := NewPermissionTree(nodes)
	if err := tree.EnsureNoCycle(101, 100); err != nil {
		t.Fatalf("valid deep tree: %v", err)
	}
	if err := tree.EnsureNoCycle(1, 100); !errors.Is(err, ErrPermissionCycle) {
		t.Fatalf("deep cycle: %v", err)
	}
	// 存量祖先链有环，即使不包含待移动节点，也必须终止并拒绝写入。
	nodes[0].parentID = 100
	if err := tree.EnsureNoCycle(101, 100); !errors.Is(err, ErrPermissionCycle) {
		t.Fatalf("existing cycle: %v", err)
	}
}
