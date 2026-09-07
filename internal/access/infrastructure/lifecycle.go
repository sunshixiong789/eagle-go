package infrastructure

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/eagle-go/eagle/pkg/authz"
	"github.com/eagle-go/eagle/pkg/healthx"
)

func NewEnforcer(store *PolicyStore) (*authz.Enforcer, error) {
	codes, err := store.PermissionCatalogCodes(context.Background())
	if err != nil {
		return nil, err
	}
	if err := authz.ValidateRegisteredPolicies(codes); err != nil {
		return nil, err
	}
	return authz.NewEnforcer(authz.NewStorageAdapter(store))
}

func RegisterPolicyHealth(store *PolicyStore, enforcer *authz.Enforcer) func() {
	health := &policyReadiness{}
	return healthx.Default.Register("authz-policy", func(ctx context.Context) error {
		return health.check(ctx, store, enforcer, time.Now())
	})
}

const policyLagGrace = 30 * time.Second

type policyReadiness struct {
	mu       sync.Mutex
	lagSince time.Time
}

func (h *policyReadiness) check(ctx context.Context, store *PolicyStore, enforcer *authz.Enforcer, now time.Time) error {
	// 串行读取与更新时间，避免并发探针用旧版本覆盖已恢复的状态。
	h.mu.Lock()
	defer h.mu.Unlock()
	version, err := store.PolicyVersion(ctx)
	if err != nil {
		return err
	}
	loaded := enforcer.LoadedPolicyVersion()
	recordPolicyVersionLag(ctx, version, loaded)
	if loaded >= version {
		h.lagSince = time.Time{}
		return nil
	}
	if h.lagSince.IsZero() {
		h.lagSince = now
	}
	if now.Sub(h.lagSince) >= policyLagGrace {
		return fmt.Errorf("authz policy remains stale: loaded=%d database=%d", loaded, version)
	}
	return nil
}

// NewPolicyReconciler starts version reconciliation and returns its cleanup.
func NewPolicyReconciler(store *PolicyStore, enforcer *authz.Enforcer, logger *slog.Logger) func() {
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runPolicyReconciler(ctx, store, enforcer, logger)
	}()
	return func() {
		cancel()
		<-done
	}
}
