package authz

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/casbin/casbin/v2/model"
)

type mutablePolicySource struct {
	rows       []StoredPolicy
	version    int64
	versionErr error
	rowsErr    error
}

func (s *mutablePolicySource) LoadPolicyRows(context.Context) ([]StoredPolicy, error) {
	return s.rows, s.rowsErr
}
func (s *mutablePolicySource) PolicyVersion(context.Context) (int64, error) {
	return s.version, s.versionErr
}

func TestReplacePolicySnapshotIsAtomicAndVersioned(t *testing.T) {
	enforcer, err := NewEnforcer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := enforcer.SetRolePermissions(context.Background(), "viewer", []string{"system:user:list"}); err != nil {
		t.Fatal(err)
	}

	rows := []StoredPolicy{
		{PType: "p", Values: []string{"editor", "system:user:add"}},
		{PType: "p", Values: []string{"viewer", "system:user:list"}},
		{PType: "g", Values: []string{"lead", "editor"}},
	}
	if err := enforcer.ReplacePolicySnapshot(context.Background(), rows, 12); err != nil {
		t.Fatal(err)
	}
	if enforcer.LoadedPolicyVersion() != 12 {
		t.Fatalf("loaded version = %d", enforcer.LoadedPolicyVersion())
	}
	if allowed, err := enforcer.Allow([]string{"lead"}, "system:user:add"); err != nil || !allowed {
		t.Fatalf("inherited permission = %v, %v", allowed, err)
	}
	permissions, err := enforcer.PermissionsOf(context.Background(), []string{"lead", "editor", "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(permissions)
	if !slices.Equal(permissions, []string{"system:user:add", "system:user:list"}) {
		t.Fatalf("permissions = %v", permissions)
	}
}

func TestReloadPolicyReplacesOldSnapshot(t *testing.T) {
	source := &mutablePolicySource{
		rows:    []StoredPolicy{{PType: "p", Values: []string{"editor", "system:user:list"}}},
		version: 1,
	}
	enforcer, err := NewEnforcer(NewStorageAdapter(source))
	if err != nil {
		t.Fatal(err)
	}
	if allowed, err := enforcer.Allow([]string{"editor"}, "system:user:list"); err != nil || !allowed {
		t.Fatalf("initial permission = %v, %v", allowed, err)
	}

	source.rows = []StoredPolicy{{PType: "p", Values: []string{"editor", "system:user:add"}}}
	source.version = 2
	if err := enforcer.ReloadPolicy(context.Background()); err != nil {
		t.Fatal(err)
	}
	if enforcer.LoadedPolicyVersion() != 2 {
		t.Fatalf("loaded version = %d", enforcer.LoadedPolicyVersion())
	}
	if allowed, _ := enforcer.Allow([]string{"editor"}, "system:user:list"); allowed {
		t.Fatal("old permission survived reload")
	}
	if allowed, err := enforcer.Allow([]string{"editor"}, "system:user:add"); err != nil || !allowed {
		t.Fatalf("new permission = %v, %v", allowed, err)
	}

	want := errors.New("policy store unavailable")
	source.rowsErr = want
	if err := enforcer.ReloadPolicy(context.Background()); !errors.Is(err, want) {
		t.Fatalf("reload error = %v", err)
	}
	// A failed reload must keep the last known-good in-memory snapshot.
	if allowed, err := enforcer.Allow([]string{"editor"}, "system:user:add"); err != nil || !allowed {
		t.Fatalf("last known-good permission = %v, %v", allowed, err)
	}
}

func TestPersistentEnforcerRejectsDirectMemoryWrites(t *testing.T) {
	source := &mutablePolicySource{version: 1}
	enforcer, err := NewEnforcer(NewStorageAdapter(source))
	if err != nil {
		t.Fatal(err)
	}
	if err := enforcer.SetRolePermissions(context.Background(), "editor", []string{"system:user:add"}); !errors.Is(err, ErrAdapterReadOnly) {
		t.Fatalf("set error = %v", err)
	}
	if err := enforcer.AddRoleInheritance(context.Background(), "lead", "editor"); !errors.Is(err, ErrAdapterReadOnly) {
		t.Fatalf("inheritance error = %v", err)
	}
}

func TestStorageAdapterLoadPolicyErrorBoundaries(t *testing.T) {
	m, err := model.NewModelFromString(ModelText)
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("source failed")
	for _, tc := range []struct {
		name   string
		source *mutablePolicySource
	}{
		{"version", &mutablePolicySource{versionErr: want}},
		{"rows", &mutablePolicySource{rowsErr: want}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := NewStorageAdapter(tc.source)
			if err := adapter.LoadPolicy(m); !errors.Is(err, want) {
				t.Fatalf("load error = %v", err)
			}
		})
	}
}
