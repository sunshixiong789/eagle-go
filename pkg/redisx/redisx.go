// Package redisx 封装 Redis 客户端的构造与通用缓存原语。
package redisx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

// Config 是 Redis 连接参数。
type Config struct {
	Addr         string
	Password     string
	DB           int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// New 建立客户端并做一次 PING 探测。
func New(ctx context.Context, cfg Config) (*redis.Client, func(), error) {
	cli, cleanup := NewClient(cfg)

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := cli.Ping(pingCtx).Err(); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("ping redis: %w", err)
	}

	return cli, cleanup, nil
}

// NewClient 构造惰性 Redis 客户端但不做连通性探测。
// 测试或可选缓存可以走这条路径；生产装配用 New，启动时确认 Redis 可用。
func NewClient(cfg Config) (*redis.Client, func()) {
	cli := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  orDefault(cfg.DialTimeout, 2*time.Second),
		ReadTimeout:  orDefault(cfg.ReadTimeout, 500*time.Millisecond),
		WriteTimeout: orDefault(cfg.WriteTimeout, 500*time.Millisecond),
	})

	return cli, func() { _ = cli.Close() }
}

func orDefault(v, def time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return v
}

// Loader 是缓存未命中时的回源函数。
type Loader[T any] func(ctx context.Context) (T, error)

// Cache 是「读缓存 -> 未命中回源 -> 回填」的通用封装，
// 并用 singleflight 合并同一 key 的并发回源，避免缓存击穿把压力直接打到库上。
//
// 泛型参数 T 是被缓存的值类型，需能被 codec 编解码。
type Cache[T any] struct {
	cli   *redis.Client
	ttl   time.Duration
	group singleflight.Group
	codec Codec[T]
}

// Codec 定义缓存值的序列化方式，调用方按需注入（JSON / protobuf / 自定义）。
type Codec[T any] interface {
	Marshal(T) ([]byte, error)
	Unmarshal([]byte) (T, error)
}

// JSONCodec 用 encoding/json 编解码缓存值。
type JSONCodec[T any] struct{}

func (JSONCodec[T]) Marshal(v T) ([]byte, error) { return json.Marshal(v) }

func (JSONCodec[T]) Unmarshal(b []byte) (T, error) {
	var v T
	err := json.Unmarshal(b, &v)
	return v, err
}

// NewCache 构造一个带 TTL 的缓存包装。
func NewCache[T any](cli *redis.Client, ttl time.Duration, codec Codec[T]) *Cache[T] {
	return &Cache[T]{cli: cli, ttl: ttl, codec: codec}
}

// Get 先查缓存，未命中则经 singleflight 回源并回填。
//
// Redis 本身故障时不会让业务失败：读失败直接降级回源，
// 写失败只影响下次命中率。认证/鉴权链路上这点很关键——
// 缓存挂了应该变慢，而不是变成全站登不上。
func (c *Cache[T]) Get(ctx context.Context, key string, load Loader[T]) (T, error) {
	var zero T

	raw, err := c.cli.Get(ctx, key).Bytes()
	switch {
	case err == nil:
		if v, decErr := c.codec.Unmarshal(raw); decErr == nil {
			recordCache(ctx, "hit")
			return v, nil
		}
		recordCache(ctx, "decode_error")
		// 解码失败说明缓存里是脏数据（多为版本升级导致的格式变更），
		// 删掉并按未命中处理
		_ = c.cli.Del(ctx, key).Err()
	case errors.Is(err, redis.Nil):
		recordCache(ctx, "miss")
		// 未命中，走下面的回源
	default:
		// Redis 不可用：降级直连数据源
		recordCache(ctx, "redis_fallback")
		return load(ctx)
	}

	v, err, _ := c.group.Do(key, func() (any, error) {
		loaded, loadErr := load(ctx)
		if loadErr != nil {
			return nil, loadErr
		}
		if b, encErr := c.codec.Marshal(loaded); encErr == nil {
			_ = c.cli.Set(ctx, key, b, c.ttl).Err()
		}
		recordCache(ctx, "loaded")
		return loaded, nil
	})
	if err != nil {
		return zero, err
	}

	typed, ok := v.(T)
	if !ok {
		return zero, fmt.Errorf("redisx: unexpected cached type %T for key %q", v, key)
	}
	return typed, nil
}

// Invalidate 主动删除若干 key。权限、字典等数据变更后调用，
// 使新配置立即生效而不必等 TTL 自然过期。
func (c *Cache[T]) Invalidate(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return c.cli.Del(ctx, keys...).Err()
}
