// Package data 是 system 服务的基础设施层：biz 层仓储接口的具体实现。
//
// 这里承担三件事：sqlc 生成类型与领域模型之间的转换、
// PostgreSQL 错误到领域错误的映射、以及 Redis 缓存的读写与失效。
package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/go-kratos/kratos/v3/log"
	"github.com/google/wire"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/redis/go-redis/v9"

	"github.com/eagle-go/eagle/app/system/internal/conf"
	"github.com/eagle-go/eagle/pkg/db"
	"github.com/eagle-go/eagle/pkg/redisx"
)

// ProviderSet 是 data 层的 wire provider 集合。
var ProviderSet = wire.NewSet(
	NewData,
	NewRedisClient,
	NewUserRepo,
	NewRoleRepo,
	NewPermissionRepo,
	NewDictRepo,
)

// NewRedisClient 把 Redis 句柄暴露给 server 层构造 token 撤销存储。
// 连接的生命周期仍归 Data 管理，这里只是共享同一个连接池，
// 不额外开一份连接。
func NewRedisClient(d *Data) *redis.Client { return d.rdb }

// Data 持有所有外部资源句柄。
type Data struct {
	db    *db.DB
	rdb   *redis.Client
	cache cacheConfig
}

type cacheConfig struct {
	permTTL time.Duration
	dictTTL time.Duration
}

// NewData 建立数据库与 Redis 连接。
// 返回的 cleanup 由 wire 串进应用退出流程。
func NewData(c *conf.Data, ac *conf.Auth) (*Data, func(), error) {
	ctx := context.Background()

	database, dbCleanup, err := db.New(ctx, db.Config{
		DSN:             c.GetDatabase().GetDsn(),
		MaxConns:        c.GetDatabase().GetMaxConns(),
		MinConns:        c.GetDatabase().GetMinConns(),
		MaxConnLifetime: c.GetDatabase().GetMaxConnLifetime().AsDuration(),
		MaxConnIdleTime: c.GetDatabase().GetMaxConnIdleTime().AsDuration(),
	})
	if err != nil {
		return nil, nil, err
	}

	rdb, redisCleanup, err := redisx.New(ctx, redisx.Config{
		Addr:         c.GetRedis().GetAddr(),
		Password:     c.GetRedis().GetPassword(),
		DB:           int(c.GetRedis().GetDb()),
		DialTimeout:  c.GetRedis().GetDialTimeout().AsDuration(),
		ReadTimeout:  c.GetRedis().GetReadTimeout().AsDuration(),
		WriteTimeout: c.GetRedis().GetWriteTimeout().AsDuration(),
	})
	if err != nil {
		dbCleanup()
		return nil, nil, err
	}

	d := &Data{
		db:  database,
		rdb: rdb,
		cache: cacheConfig{
			permTTL: orDefault(ac.GetPermCacheTtl().AsDuration(), 10*time.Minute),
			dictTTL: orDefault(ac.GetDictCacheTtl().AsDuration(), 30*time.Minute),
		},
	}

	cleanup := func() {
		log.Info("closing system data resources")
		redisCleanup()
		dbCleanup()
	}
	return d, cleanup, nil
}

func orDefault(v, def time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return v
}

// ── 缓存键 ────────────────────────────────────────────────

const (
	keyPrefixUserPerm = "eagle:perm:u:"
	keyPrefixDictData = "eagle:dict:"
)

func userPermKey(userID int64) string {
	return keyPrefixUserPerm + strconv.FormatInt(userID, 10)
}

func dictDataKey(dictType string) string {
	return keyPrefixDictData + dictType
}

// ── 错误映射 ──────────────────────────────────────────────

// PostgreSQL 错误码。
const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
)

// isNoRows 判断是否为「查无此行」。
func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// isUniqueViolation 判断是否违反唯一约束。
// 用它把并发插入产生的冲突翻译成领域层的「已存在」，
// 而不是把裸的数据库错误抛给调用方。
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation
}

// isForeignKeyViolation 判断是否违反外键约束。
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgForeignKeyViolation
}

// ── 类型转换 ──────────────────────────────────────────────

// 数据库用 smallint（int16）存状态，领域模型和 proto 用 int32。
// 收敛到这两个函数，避免转换散落各处。
func toInt16(v int32) int16 { return int16(v) }
func toInt32(v int16) int32 { return int32(v) }

// nilIfEmpty 把空字符串转成 nil，供 sqlc 的可选过滤参数使用。
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// int32PtrToInt16Ptr 转换可选的状态过滤条件。
func int32PtrToInt16Ptr(v *int32) *int16 {
	if v == nil {
		return nil
	}
	i := int16(*v)
	return &i
}

// ── 通用缓存原语 ──────────────────────────────────────────

// jsonCodec 用 JSON 编解码缓存值。
// 权限码和字典项都是小结构，JSON 的开销可以忽略，
// 换来的是缓存内容可以直接用 redis-cli 读懂，排障方便。
type jsonCodec[T any] struct{}

func (jsonCodec[T]) Marshal(v T) ([]byte, error) { return json.Marshal(v) }

func (jsonCodec[T]) Unmarshal(b []byte) (T, error) {
	var v T
	err := json.Unmarshal(b, &v)
	return v, err
}

// cached 读缓存，未命中则回源并回填。
// Redis 故障时降级为直连数据库——鉴权链路上缓存挂掉应该变慢，而不是全站不可用。
func cached[T any](ctx context.Context, d *Data, key string, ttl time.Duration, load func(context.Context) (T, error)) (T, error) {
	c := redisx.NewCache[T](d.rdb, ttl, jsonCodec[T]{})
	return c.Get(ctx, key, load)
}

// invalidate 删除若干缓存键。
func (d *Data) invalidate(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	if err := d.rdb.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("invalidate cache: %w", err)
	}
	return nil
}

// invalidateByPrefix 按前缀批量删除。
//
// 用 SCAN 而不是 KEYS：KEYS 会阻塞 Redis 单线程，
// 在键多的实例上足以造成全站抖动。
func (d *Data) invalidateByPrefix(ctx context.Context, prefix string) error {
	var cursor uint64
	for {
		keys, next, err := d.rdb.Scan(ctx, cursor, prefix+"*", 512).Result()
		if err != nil {
			return fmt.Errorf("scan %q: %w", prefix, err)
		}
		if len(keys) > 0 {
			if err := d.rdb.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("delete keys under %q: %w", prefix, err)
			}
		}
		if next == 0 {
			return nil
		}
		cursor = next
	}
}
