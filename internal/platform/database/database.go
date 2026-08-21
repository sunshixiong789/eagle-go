// Package database owns the shared database connection and PostgreSQL error
// classification. Business repositories stay inside their modules.
package database

import (
	"context"
	"errors"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/go-kratos/kratos/v3/log"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eagle-go/eagle/ent"
	"github.com/eagle-go/eagle/internal/platform/config"
	"github.com/eagle-go/eagle/pkg/db"
	"github.com/eagle-go/eagle/pkg/healthx"
)

// Database owns the Ent client and connection lifecycle.
type Database struct {
	client         *ent.Client
	healthCleanups []func()
}

// Open establishes the shared PostgreSQL connection.
func Open(c *config.Data) (*Database, func(), error) {
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

	d := &Database{client: client}
	d.healthCleanups = append(d.healthCleanups,
		healthx.Default.Register("postgres", func(ctx context.Context) error {
			_, err := client.PolicyState.Query().Exist(ctx)
			return err
		}),
	)

	cleanup := func() {
		log.Info("closing database")
		for _, unregister := range d.healthCleanups {
			unregister()
		}
		dbCleanup()
	}
	return d, cleanup, nil
}

// Client exposes Ent only to module infrastructure packages.
func (d *Database) Client() *ent.Client { return d.client }

// ── 错误映射 ──────────────────────────────────────────────

// PostgreSQL 错误码（SQLSTATE）。
const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
)

// isNotFound 判断是否为「查无此行」。
func IsNotFound(err error) bool {
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
func IsUniqueViolation(err error) bool {
	if pgErrorCode(err) == pgUniqueViolation {
		return true
	}
	// 兜底：英文 locale 下 ent 自己能识别出来
	return ent.IsConstraintError(err) && pgErrorCode(err) == ""
}

// isForeignKeyViolation 判断是否违反外键约束。
func IsForeignKeyViolation(err error) bool {
	return pgErrorCode(err) == pgForeignKeyViolation
}
