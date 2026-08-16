package redisx

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestCacheSingleflightIsSharedAcrossConcurrentGets(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewCache[string](client, time.Minute, JSONCodec[string]{})

	var loads atomic.Int32
	loader := func(context.Context) (string, error) {
		loads.Add(1)
		time.Sleep(50 * time.Millisecond)
		return "value", nil
	}

	const callers = 20
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			value, err := cache.Get(context.Background(), "shared-key", loader)
			if err != nil || value != "value" {
				t.Errorf("Get = %q, %v", value, err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if got := loads.Load(); got != 1 {
		t.Fatalf("loader called %d times, want 1", got)
	}
}
