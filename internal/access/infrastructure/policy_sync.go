package infrastructure

import (
	"context"
	"log/slog"
	"time"

	"github.com/eagle-go/eagle/pkg/authz"
)

const policyReconcileInterval = 5 * time.Second

// runPolicyReconciler 每个周期对账；数据库或重载失败时持续重试，
// 持续版本落后由 readiness 的容忍窗口约束。
func runPolicyReconciler(
	ctx context.Context,
	store *PolicyStore,
	enforcer *authz.Enforcer,
	logger *slog.Logger,
) {
	ticker := time.NewTicker(policyReconcileInterval)
	defer ticker.Stop()

	reconcile := func() {
		attemptCtx, cancel := context.WithTimeout(ctx, policyReconcileInterval)
		defer cancel()
		version, err := store.PolicyVersion(attemptCtx)
		if err != nil {
			if ctx.Err() == nil {
				logger.ErrorContext(ctx, "authz: 对账时读取策略版本失败", "error", err)
			}
			return
		}
		recordPolicyVersionLag(ctx, version, enforcer.LoadedPolicyVersion())
		if version == enforcer.LoadedPolicyVersion() {
			return
		}
		if err := enforcer.ReloadPolicy(attemptCtx); err != nil {
			if ctx.Err() == nil {
				logger.ErrorContext(ctx, "authz: 对账重载策略失败", "error", err, "database_version", version, "loaded_version", enforcer.LoadedPolicyVersion())
			}
			return
		}
		logger.InfoContext(ctx, "authz: 策略版本已自动追平", "version", version)
	}

	reconcile()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcile()
		}
	}
}
