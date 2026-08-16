package authz

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// reloadRecorder 是 Reloadable 的测试替身，记录被调用的次数。
type reloadRecorder struct {
	mu    sync.Mutex
	count int
}

func (r *reloadRecorder) ReloadPolicy(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.count++
	return nil
}

func (r *reloadRecorder) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}

// TestRedisWatcherReloadsOnNotify 验证广播-订阅这条链路本身：
// Notify 发出的消息能让另一端 Watch 里的 target 收到重载调用。
//
// Watch 的订阅是异步建立的，测试起跑时不确定它是否已经连上，
// 因此用"反复 Notify 直到观察到重载"而不是"Notify 一次就断言"，
// 这样断的是行为契约（最终会同步），不会因为订阅建立的时序而抖动。
func TestRedisWatcherReloadsOnNotify(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("启动 miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	w := NewRedisWatcher(rdb, "", nil)
	target := &reloadRecorder{}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go w.Watch(ctx, target)

	deadline := time.Now().Add(2 * time.Second)
	for target.Count() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("超时：Watch 未在收到广播后触发重载")
		}
		w.Notify(context.Background())
		time.Sleep(20 * time.Millisecond)
	}
}

// TestRedisWatcherWatchStopsOnContextCancel 验证 ctx 取消后 Watch 会退出，
// 不会在应用关闭后继续占着一个 goroutine 和一条 Redis 连接。
func TestRedisWatcherWatchStopsOnContextCancel(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("启动 miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	w := NewRedisWatcher(rdb, "", nil)
	target := &reloadRecorder{}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Watch(ctx, target)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("超时：ctx 取消后 Watch 仍未返回")
	}
}

// TestRedisWatcherNotifyNilReceiverIsNoop 验证未装配 Redis 的调用方
// （比如某些不关心多副本同步的测试）可以安全地在 nil watcher 上调用 Notify。
func TestRedisWatcherNotifyNilReceiverIsNoop(t *testing.T) {
	var w *RedisWatcher
	w.Notify(context.Background()) // 不应 panic
}

func TestRedisWatcherPublishNilReceiverReturnsError(t *testing.T) {
	var w *RedisWatcher
	if err := w.PublishVersion(context.Background(), 1); err == nil {
		t.Fatal("reliable publish without watcher should fail")
	}
}
