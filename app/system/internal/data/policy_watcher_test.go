package data

import (
	"context"
	"testing"
	"time"

	"github.com/eagle-go/eagle/ent/casbinrule"
)

// TestPolicyWatcherSyncsAcrossReplicas 模拟多副本部署：两个进程各自持有
// 独立的内存 Casbin 模型，但共享同一个数据库和同一个 Redis。
//
// 副本 B 的内存模型在副本 A 写入之前就已经加载好了。没有 RedisWatcher
// 的话，B 会一直用旧策略判定，直到进程重启，且不会有任何报错——
// 这正是 data.NewPolicyWatcher 要补上的缺口（见 data.go 的注释）。
// 本用例验证的是"已经在跑的 B 会被广播追上"，不是"新建的 B 能读到最新数据"
// ——后者 TestPolicyStoreSetRolePermissionsPersists 已经覆盖，
// 不依赖广播机制，测不出这里要修的问题。
func TestPolicyWatcherSyncsAcrossReplicas(t *testing.T) {
	skipIfShort(t)

	ctx := context.Background()
	const role = "test-multi-replica-role"
	const permCode = "system:dict:add"

	t.Cleanup(func() {
		_, _ = testData.client.CasbinRule.Delete().
			Where(casbinrule.V0EQ(role)).Exec(ctx)
	})

	// 两个副本都走生产用的 NewPolicyWatcher，而不是直接摆弄 authz.RedisWatcher：
	// 这样测的是实际装配路径，不是底层机制本身（那是 pkg/authz 的单测负责的）。

	// 副本 B：先加载、先订阅。此刻数据库里还没有这条策略。
	enforcerB, err := NewEnforcer(testData.client)
	if err != nil {
		t.Fatalf("构造副本 B 的 enforcer: %v", err)
	}
	_, cleanupB, err := NewPolicyWatcher(redisClient(), enforcerB, nil)
	if err != nil {
		t.Fatalf("构造副本 B 的 watcher: %v", err)
	}
	t.Cleanup(cleanupB)

	if ok, err := enforcerB.Allow([]string{role}, permCode); err != nil {
		t.Fatalf("Allow(写入前): %v", err)
	} else if ok {
		t.Fatal("写入前不应已经放行")
	}

	// 副本 A：处理管理员的改权限请求。
	enforcerA, err := NewEnforcer(testData.client)
	if err != nil {
		t.Fatalf("构造副本 A 的 enforcer: %v", err)
	}
	watcherA, cleanupA, err := NewPolicyWatcher(redisClient(), enforcerA, nil)
	if err != nil {
		t.Fatalf("构造副本 A 的 watcher: %v", err)
	}
	t.Cleanup(cleanupA)
	storeA := NewPolicyRepo(enforcerA, testData.client, watcherA)

	// 反复写入直到副本 B 追上：Watch 的订阅是异步建立的，用重试而不是
	// "写一次就断言"，断的是"最终会同步"这个行为契约，不受订阅时序影响。
	// SaveBinding 对同一角色是全量覆盖语义，重复调用是幂等的。
	deadline := time.Now().Add(2 * time.Second)
	for {
		binding := mustBinding(t, role, permCode)
		if err := storeA.SaveBinding(ctx, binding); err != nil {
			t.Fatalf("副本 A SaveBinding: %v", err)
		}

		ok, err := enforcerB.Allow([]string{role}, permCode)
		if err != nil {
			t.Fatalf("Allow(等待同步): %v", err)
		}
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("超时：副本 B 未通过广播同步到副本 A 写入的策略")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
