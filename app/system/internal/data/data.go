// Package data 是 system 服务的基础设施层：domain 层仓储接口的具体实现。
//
// 承担三件事：ent 生成类型与领域模型之间的转换、
// ent 错误到领域错误的映射、以及 Redis 缓存的读写与失效。
package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-kratos/kratos/v3/log"
	"github.com/google/wire"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/redis/go-redis/v9"

	"github.com/eagle-go/eagle/app/system/internal/conf"
	"github.com/eagle-go/eagle/ent"
	"github.com/eagle-go/eagle/pkg/authz"
	"github.com/eagle-go/eagle/pkg/db"
	"github.com/eagle-go/eagle/pkg/redisx"
)

// ProviderSet 是 data 层的 wire provider 集合。
var ProviderSet = wire.NewSet(
	NewData,
	NewEntClient,
	NewRedisClient,
	NewEnforcer,
	NewPolicyWatcher,
	NewPolicyRepo,
	NewPermissionRepo,
	NewDictRepo,
)

// Data 持有所有外部资源句柄。
type Data struct {
	client *ent.Client
	rdb    *redis.Client
	cache  cacheConfig
}

type cacheConfig struct {
	dictTTL time.Duration
}

// NewData 建立数据库与 Redis 连接。
// 返回的 cleanup 由 wire 串进应用退出流程。
func NewData(c *conf.Data, ac *conf.Auth) (*Data, func(), error) {
	ctx := context.Background()

	client, dbCleanup, err := db.New(ctx, db.Config{
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
		client: client,
		rdb:    rdb,
		cache: cacheConfig{
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

// NewEntClient 暴露 ent 客户端，供 Casbin 适配器复用同一连接池。
func NewEntClient(d *Data) *ent.Client { return d.client }

// NewRedisClient 把 Redis 句柄暴露给 server 层构造 token 撤销存储。
// 连接生命周期仍归 Data 管理，这里只共享同一个连接池。
func NewRedisClient(d *Data) *redis.Client { return d.rdb }

// NewEnforcer 构造 Casbin 判定器，策略存储复用项目自身的 ent 客户端。
func NewEnforcer(client *ent.Client) (*authz.Enforcer, error) {
	return authz.NewEnforcer(authz.NewEntAdapter(client))
}

// NewPolicyWatcher 构造策略广播器，并在后台启动订阅循环，
// 使多副本部署下各实例的内存 Casbin 模型保持同步。
//
// 没有这层同步，SetRolePermissions 只更新处理该次请求的那个副本：
// 后台改角色权限只有命中的副本立即生效，其余副本要等进程重启才追上，
// 且不会有任何报错——只会表现为「改了权限，一部分用户生效一部分不生效」。
//
// 返回的 cleanup 停止订阅循环并等它真正退出，由 wire 串进应用退出流程。
//
// 等待退出而不是取消了事：cleanup 之间是有序的（wire 按构造的反序执行），
// cleanup 一返回，后续 cleanup 就可能去关数据库连接池。如果这里只是
// cancel 而不等 goroutine 真正停下来，它手上可能还有一次正在跑的
// ReloadPolicy，会撞上刚被关闭的连接——现象是退出时打一条无害但唬人的
// 错误日志。等 goroutine 确认退出后再返回，就不存在这个时间窗口。
func NewPolicyWatcher(rdb *redis.Client, enforcer *authz.Enforcer, logger *slog.Logger) (*authz.RedisWatcher, func(), error) {
	w := authz.NewRedisWatcher(rdb, "", logger)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Watch(ctx, enforcer)
	}()

	cleanup := func() {
		cancel()
		<-done
	}
	return w, cleanup, nil
}

func orDefault(v, def time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return v
}

// ── 缓存键 ────────────────────────────────────────────────

const keyPrefixDictData = "eagle:dict:"

func dictDataKey(dictType string) string {
	return keyPrefixDictData + dictType
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

// ── 通用缓存原语 ──────────────────────────────────────────

// jsonCodec 用 JSON 编解码缓存值。
// 字典项是小结构，JSON 开销可忽略，换来缓存内容可以直接用
// redis-cli 读懂，排障方便。
type jsonCodec[T any] struct{}

func (jsonCodec[T]) Marshal(v T) ([]byte, error) { return json.Marshal(v) }

func (jsonCodec[T]) Unmarshal(b []byte) (T, error) {
	var v T
	err := json.Unmarshal(b, &v)
	return v, err
}

// cached 读缓存，未命中则回源并回填。
// Redis 故障时降级为直连数据库——缓存挂掉应该变慢，而不是全站不可用。
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
