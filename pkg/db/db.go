// Package db 提供 PostgreSQL 连接池与事务封装。
//
// 这里刻意只依赖一个朴素的 Config 结构体而不是某个服务的 conf proto，
// 使 auth 和 system 能共用同一份实现，pkg 不反向依赖 app。
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/eagle-go/eagle/pkg/db/sqlc"
)

// Config 是连接池参数。零值字段会落到下面的默认值。
type Config struct {
	DSN             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

// DB 持有连接池，并内嵌 sqlc 生成的 Queries，
// 使调用方在不需要事务时可以直接 d.GetUserByID(...)。
type DB struct {
	*sqlc.Queries
	Pool *pgxpool.Pool
}

// New 建立连接池并做一次连通性探测。
// 返回的 cleanup 需要由调用方（wire 的 cleanup 链）在退出时执行。
func New(ctx context.Context, cfg Config) (*DB, func(), error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, nil, fmt.Errorf("parse postgres dsn: %w", err)
	}

	if cfg.MaxConns > 0 {
		poolCfg.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		poolCfg.MinConns = cfg.MinConns
	}
	if cfg.MaxConnLifetime > 0 {
		poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	}
	if cfg.MaxConnIdleTime > 0 {
		poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("create postgres pool: %w", err)
	}

	// 启动即失败优于跑起来之后第一个请求才发现连不上库
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("ping postgres: %w", err)
	}

	d := &DB{Queries: sqlc.New(pool), Pool: pool}
	return d, pool.Close, nil
}

// Tx 在一个事务里执行 fn。fn 返回 error 或 panic 都会回滚。
//
// 用法：
//
//	err := d.Tx(ctx, func(q *sqlc.Queries) error {
//	    if _, err := q.CreateUser(ctx, params); err != nil {
//	        return err
//	    }
//	    return q.AddUserRole(ctx, roleParams)
//	})
func (d *DB) Tx(ctx context.Context, fn func(q *sqlc.Queries) error) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	// panic 时也要回滚，否则连接会被占住直到超时
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()

	if err := fn(d.Queries.WithTx(tx)); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errIsTxClosed(rbErr) {
			return fmt.Errorf("%w (rollback failed: %v)", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// 事务已被提交或回滚时 Rollback 会返回 pgx.ErrTxClosed，
// 这不算错误，不应盖掉业务层真正的失败原因。
func errIsTxClosed(err error) bool {
	return errors.Is(err, pgx.ErrTxClosed)
}
