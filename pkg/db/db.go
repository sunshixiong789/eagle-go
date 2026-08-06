// Package db 构造 ent 客户端并提供事务封装。
//
// 只依赖一个朴素的 Config 结构体而不是某个服务的 conf proto，
// 使多个服务能共用同一份实现，pkg 不反向依赖 app。
package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	// 注册 pgx 的 database/sql 驱动。ent 走 database/sql 接口，
	// 但底层仍是 pgx 的实现，因此能拿到 pgx 的协议层性能。
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eagle-go/eagle/ent"
)

// Config 是连接池参数。零值字段会落到下面的默认值。
type Config struct {
	DSN             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

// New 建立 ent 客户端并做一次连通性探测。
// 返回的 cleanup 需由调用方（wire 的 cleanup 链）在退出时执行。
func New(ctx context.Context, cfg Config) (*ent.Client, func(), error) {
	sqlDB, err := sql.Open("pgx", cfg.DSN)
	if err != nil {
		return nil, nil, fmt.Errorf("open postgres: %w", err)
	}

	if cfg.MaxConns > 0 {
		sqlDB.SetMaxOpenConns(int(cfg.MaxConns))
	}
	if cfg.MinConns > 0 {
		sqlDB.SetMaxIdleConns(int(cfg.MinConns))
	}
	if cfg.MaxConnLifetime > 0 {
		sqlDB.SetConnMaxLifetime(cfg.MaxConnLifetime)
	}
	if cfg.MaxConnIdleTime > 0 {
		sqlDB.SetConnMaxIdleTime(cfg.MaxConnIdleTime)
	}

	// 启动即失败优于跑起来之后第一个请求才发现连不上库
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("ping postgres: %w", err)
	}

	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, sqlDB)))
	cleanup := func() { _ = client.Close() }

	return client, cleanup, nil
}

// WithTx 在一个事务里执行 fn。fn 返回 error 或 panic 都会回滚。
//
// 用法：
//
//	err := db.WithTx(ctx, client, func(tx *ent.Tx) error {
//	    if _, err := tx.Permission.Create().Save(ctx); err != nil {
//	        return err
//	    }
//	    return nil
//	})
func WithTx(ctx context.Context, client *ent.Client, fn func(tx *ent.Tx) error) error {
	tx, err := client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	// panic 时也要回滚，否则连接会被占住直到超时
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("%w (rollback failed: %v)", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
