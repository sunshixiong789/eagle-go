package data

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eagle-go/eagle/pkg/authz"
)

const policyReconcileInterval = 5 * time.Second

// runPolicyReconciler 同时承担两个恢复职责：发布尚未投递的 Outbox，
// 以及比对数据库版本与内存版本。即使 Redis Pub/Sub 在断线期间丢消息，
// 副本也会在一个对账周期内自动追上。
func runPolicyReconciler(
	ctx context.Context,
	enforcer *authz.Enforcer,
	watcher *authz.RedisWatcher,
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
		if err := publishPolicyOutbox(ctx, adapter, watcher); err != nil && ctx.Err() == nil {
			logger.ErrorContext(ctx, "authz: 发布策略 Outbox 失败", "error", err)
		}
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

func publishPolicyOutbox(ctx context.Context, adapter *authz.EntAdapter, watcher *authz.RedisWatcher) error {
	events, err := adapter.PendingPolicyEvents(ctx, 100)
	if err != nil {
		return fmt.Errorf("query pending policy outbox: %w", err)
	}
	for _, event := range events {
		if err := watcher.PublishVersion(ctx, event.PolicyVersion); err != nil {
			recordOutboxEvent(ctx, "publish_error")
			if markErr := adapter.MarkPolicyEventFailed(ctx, event.ID, err); markErr != nil {
				return fmt.Errorf("publish policy outbox %d: %v; record failure: %w", event.ID, err, markErr)
			}
			return fmt.Errorf("publish policy outbox %d: %w", event.ID, err)
		}
		if err := adapter.MarkPolicyEventPublished(ctx, event.ID, time.Now()); err != nil {
			recordOutboxEvent(ctx, "mark_error")
			return fmt.Errorf("mark policy outbox %d published: %w", event.ID, err)
		}
		recordOutboxEvent(ctx, "published")
	}
	return nil
}
