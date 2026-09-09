package authz

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

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

// pausedReloadAdapter 在稳定快照加载后暂停第二次调用，模拟发布前被调度挂起。
type pausedReloadAdapter struct {
	*StorageAdapter
	calls  atomic.Int64
	loaded chan struct{}
	resume chan struct{}
}

func (a *pausedReloadAdapter) LoadPolicyContext(ctx context.Context, m model.Model) error {
	call := a.calls.Add(1)
	if err := a.StorageAdapter.LoadPolicyContext(ctx, m); err != nil {
		return err
	}
	if call == 2 {
		close(a.loaded)
		<-a.resume
	}
	return nil
}

func TestConcurrentReloadPreservesRevocationAndVersion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		source := &mutablePolicySource{
			version: 1,
			rows:    []StoredPolicy{{PType: "p", Values: []string{"editor", "system:user:add"}}},
		}
		adapter := &pausedReloadAdapter{
			StorageAdapter: NewStorageAdapter(source),
			loaded:         make(chan struct{}), resume: make(chan struct{}),
		}
		release := sync.OnceFunc(func() { close(adapter.resume) })
		defer release()
		enforcer, err := NewEnforcer(adapter)
		if err != nil {
			t.Fatal(err)
		}
		source.version = 2
		first := make(chan error, 1)
		go func() { first <- enforcer.ReloadPolicy(ctx) }()
		<-adapter.loaded

		// 模拟数据库提交收权；当前重载仍持有允许访问的版本 2。
		source.version = 3
		source.rows = nil
		second := make(chan error, 1)
		go func() { second <- enforcer.ReloadPolicy(ctx) }()
		synctest.Wait()
		if calls := adapter.calls.Load(); calls != 2 {
			t.Errorf("another reload read the shared adapter before publication: calls=%d", calls)
		}
		// 加载等待期间仍能读取旧快照，不能把数据库延迟传导到全部鉴权请求。
		if allowed, err := enforcer.Allow([]string{"editor"}, "system:user:add"); err != nil || !allowed {
			t.Fatalf("decision during reload = %v, %v", allowed, err)
		}

		canceledCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		canceled := make(chan error, 1)
		go func() { canceled <- enforcer.ReloadPolicy(canceledCtx) }()
		synctest.Wait()
		cancel()
		if err := <-canceled; !errors.Is(err, context.Canceled) {
			t.Errorf("waiting reload cancellation = %v", err)
		}

		release()
		if err := <-first; err != nil {
			t.Fatal(err)
		}
		if err := <-second; err != nil {
			t.Fatal(err)
		}
		if version := enforcer.LoadedPolicyVersion(); version != 3 {
			t.Fatalf("loaded version = %d, want 3", version)
		}
		if allowed, err := enforcer.Allow([]string{"editor"}, "system:user:add"); err != nil || allowed {
			t.Fatalf("revoked permission after concurrent reload = %v, %v", allowed, err)
		}
	})
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
