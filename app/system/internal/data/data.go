// Package data 是 system 服务的基础设施层：domain 层仓储接口的具体实现。
//
// 承担 ent 生成类型与领域模型之间的转换、事务和错误映射。
package data

import (
	"context"
	"errors"
	"log/slog"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/go-kratos/kratos/v3/log"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eagle-go/eagle/app/system/internal/conf"
	"github.com/eagle-go/eagle/ent"
	"github.com/eagle-go/eagle/pkg/authz"
	"github.com/eagle-go/eagle/pkg/db"
	"github.com/eagle-go/eagle/pkg/healthx"
)

// Data 持有所有外部资源句柄。
type Data struct {
	client         *ent.Client
	healthCleanups []func()
}

// NewData 建立数据库连接。
// 返回的 cleanup 由应用装配层在退出时调用。
func NewData(c *conf.Data) (*Data, func(), error) {
	ctx := context.Background()

	sqlDB, dbCleanup, err := db.New(ctx, db.Config{
		DSN:             c.GetDatabase().GetDsn(),
		MaxConns:        c.GetDatabase().GetMaxConns(),
		MaxIdleConns:    c.GetDatabase().GetMaxIdleConns(),
		MaxConnLifetime: c.GetDatabase().GetMaxConnLifetime().AsDuration(),
		MaxConnIdleTime: c.GetDatabase().GetMaxConnIdleTime().AsDuration(),
	})
	if err != nil {
		return nil, nil, err
	}
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, sqlDB)))

	d := &Data{client: client}
	d.healthCleanups = append(d.healthCleanups,
		healthx.Default.Register("postgres", func(ctx context.Context) error {
			_, err := client.PolicyState.Query().Exist(ctx)
			return err
		}),
	)

	cleanup := func() {
		log.Info("closing system data resources")
		for _, unregister := range d.healthCleanups {
			unregister()
		}
		dbCleanup()
	}
	return d, cleanup, nil
}

// NewEntClient 暴露 ent 客户端，供 Casbin 适配器复用同一连接池。
func NewEntClient(d *Data) *ent.Client { return d.client }

// NewEnforcer 构造 Casbin 判定器，策略存储复用项目自身的 ent 客户端。
func NewEnforcer(store *policyStore) (*authz.Enforcer, error) {
	codes, err := store.PermissionCatalogCodes(context.Background())
	if err != nil {
		return nil, err
	}
	if err := authz.ValidateRegisteredPolicies(codes); err != nil {
		return nil, err
	}
	enforcer, err := authz.NewEnforcer(authz.NewStorageAdapter(store))
	if err != nil {
		return nil, err
	}
	return enforcer, nil
}

// RegisterPolicyHealth 把策略存储探活绑定到应用生命周期。
func RegisterPolicyHealth(store *policyStore, enforcer *authz.Enforcer) func() {
	return healthx.Default.Register("authz-policy", func(ctx context.Context) error {
		return checkAuthzPolicyReady(ctx, store, enforcer)
	})
}

// checkAuthzPolicyReady 是服务就绪检查里注册的策略探活。
// 读不到数据库版本视为不健康；内存版本落后只记指标，不挡流量。
func checkAuthzPolicyReady(ctx context.Context, store *policyStore, enforcer *authz.Enforcer) error {
	version, err := store.PolicyVersion(ctx)
	if err != nil {
		return err
	}
	recordPolicyVersionLag(ctx, version, enforcer.LoadedPolicyVersion())
	return nil
}

// NewPolicyReconciler starts the database-version reconciliation loop used by
// every replica and returns a cleanup that waits for it to stop.
func NewPolicyReconciler(
	store *policyStore,
	enforcer *authz.Enforcer,
	logger *slog.Logger,
) func() {
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

// ── 错误映射 ──────────────────────────────────────────────

// PostgreSQL 错误码（SQLSTATE）。
const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
)

// isNotFound 判断是否为「查无此行」。
func isNotFound(err error) bool {
	return ent.IsNotFound(err)
}

// pgErrorCode 取出 PostgreSQL 的 SQLSTATE，非数据库错误返回空串。
//
// 不用 ent.IsConstraintError：它靠匹配数据库返回的错误文本来判断，
// 在非英文 locale 的 PostgreSQL 上会静默失效——服务器把错误信息
// 本地化成中文后，字符串匹配不到，唯一冲突就会被当成未知错误
// 直接抛给调用方（本项目的集成测试正是这样发现的）。
// SQLSTATE 是标准化的数值码，与语言环境无关。
func pgErrorCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// isUniqueViolation 判断是否违反唯一约束。
// 用它把并发插入产生的冲突翻译成领域层的「已存在」。
func isUniqueViolation(err error) bool {
	if pgErrorCode(err) == pgUniqueViolation {
		return true
	}
	// 兜底：英文 locale 下 ent 自己能识别出来
	return ent.IsConstraintError(err) && pgErrorCode(err) == ""
}

// isForeignKeyViolation 判断是否违反外键约束。
func isForeignKeyViolation(err error) bool {
	return pgErrorCode(err) == pgForeignKeyViolation
}
