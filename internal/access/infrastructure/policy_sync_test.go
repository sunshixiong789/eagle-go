package infrastructure

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eagle-go/eagle/internal/platform/database/ent/casbinrule"
	"github.com/eagle-go/eagle/pkg/authz"
)

type unavailablePolicySource struct {
	*PolicyStore
	unavailable bool
}

func (s *unavailablePolicySource) LoadPolicyRows(ctx context.Context) ([]authz.StoredPolicy, error) {
	if s.unavailable {
		return nil, errors.New("policy reader unavailable")
	}
	return s.PolicyStore.LoadPolicyRows(ctx)
}

func TestCommittedPolicySucceedsWhenReloadFails(t *testing.T) {
	skipIfShort(t)
	ctx := context.Background()
	const role, code = "test-reload-failure", "system:dict:add"
	store := NewPolicyStore(testDB)
	source := &unavailablePolicySource{PolicyStore: store}
	enforcer, err := authz.NewEnforcer(authz.NewStorageAdapter(source))
	if err != nil {
		t.Fatal(err)
	}
	repo := NewPolicyRepo(enforcer, store)
	t.Cleanup(func() {
		_, _ = testDB.Client().CasbinRule.Delete().Where(casbinrule.V0EQ(role)).Exec(ctx)
	})
	source.unavailable = true
	version, err := repo.SaveBinding(ctx, mustBinding(t, role, code), nil)
	if err != nil {
		t.Fatalf("committed write reported as failed: %v", err)
	}
	binding, err := repo.FindBinding(ctx, "test-reload-failure")
	if err != nil || binding.Revision() != version || binding.IsEmpty() {
		t.Fatalf("committed version missing: binding=%v version=%d error=%v", binding, version, err)
	}
	if allowed, err := enforcer.Allow([]string{role}, code); err != nil || allowed {
		t.Fatalf("failed reload changed policy: allowed=%v error=%v", allowed, err)
	}
	source.unavailable = false
	if err := enforcer.ReloadPolicy(ctx); err != nil {
		t.Fatal(err)
	}
	if allowed, err := enforcer.Allow([]string{role}, code); err != nil || !allowed || enforcer.LoadedPolicyVersion() != version {
		t.Fatalf("policy did not recover: allowed=%v version=%d error=%v", allowed, enforcer.LoadedPolicyVersion(), err)
	}
}

func TestPolicyReconcilerSyncsAcrossReplicas(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	const role = "test-multi-replica-role"
	const permCode = "system:dict:add"

	t.Cleanup(func() {
		_, _ = testDB.Client().CasbinRule.Delete().
			Where(casbinrule.V0EQ(role)).Exec(ctx)
	})

	// 副本 B 先加载。此刻数据库里还没有这条策略。
	store := NewPolicyStore(testDB)
	enforcerB, err := NewEnforcer(store)
	if err != nil {
		t.Fatalf("构造副本 B 的 enforcer: %v", err)
	}
	t.Cleanup(NewPolicyReconciler(store, enforcerB, nil))

	if ok, err := enforcerB.Allow([]string{role}, permCode); err != nil {
		t.Fatalf("Allow(写入前): %v", err)
	} else if ok {
		t.Fatal("写入前不应已经放行")
	}

	// 副本 A 写一次；副本 B 只靠数据库版本对账追平。
	enforcerA, err := NewEnforcer(store)
	if err != nil {
		t.Fatalf("构造副本 A 的 enforcer: %v", err)
	}
	storeA := NewPolicyRepo(enforcerA, store)
	if _, err := storeA.SaveBinding(ctx, mustBinding(t, role, permCode), nil); err != nil {
		t.Fatalf("副本 A SaveBinding: %v", err)
	}

	deadline := time.Now().Add(7 * time.Second)
	for {
		ok, err := enforcerB.Allow([]string{role}, permCode)
		if err != nil {
			t.Fatalf("Allow(等待同步): %v", err)
		}
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("超时：副本 B 未通过版本对账同步到副本 A 写入的策略")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
