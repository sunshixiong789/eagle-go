package data

import (
	"context"
	"log/slog"
	"time"

	"github.com/eagle-go/eagle/pkg/authz"
)

const policyReconcileInterval = 5 * time.Second

// runPolicyReconciler 比对数据库版本与内存版本。即使 Redis Pub/Sub
// 在断线期间丢消息，副本也会在一个对账周期内自动追上。
func runPolicyReconciler(
	ctx context.Context,
	enforcer *authz.Enforcer,
	logger *slog.Logger,
) {
	adapter, ok := enforcer.EntAdapter()
	if !ok {
		logger.ErrorContext(ctx, "authz: 判定器没有可对账的策略存储")
		return
	}
	ticker := time.NewTicker(policyReconcileInterval)
	defer ticker.Stop()

	reconcile := func() {
		version, err := adapter.PolicyVersion(ctx)
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
		if err := enforcer.ReloadPolicy(ctx); err != nil {
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
