package authz

import (
	"context"
	"errors"
	"log/slog"
	"strconv"

	"github.com/redis/go-redis/v9"
)

// DefaultWatchChannel 是策略变更广播使用的默认 Redis 频道。
const DefaultWatchChannel = "eagle:authz:policy-changed"

// Reloadable 是能够从存储重新加载策略的判定器。*Enforcer 实现了它。
//
// 用接口而不是直接依赖 *Enforcer：RedisWatcher 只关心"收到通知后重载"这一件事，
// 不需要知道 Enforcer 的其余能力，测试也因此不必搭一个真判定器。
type Reloadable interface {
	ReloadPolicy(ctx context.Context) error
}

// RedisWatcher 让多个服务副本的内存 Casbin 模型保持一致。
//
// Casbin 判定完全走内存、不查库（见 Enforcer 的注释），这是关键路径上的
// 性能取舍。代价是本副本 ReloadPolicy 之后，其它副本不会自动看到新策略——
// 没有这层同步，后台改角色权限只有命中的副本立即生效，其余副本要等进程
// 重启才追上。这种失败没有任何报错，表现为"改了权限，一部分用户生效
// 一部分不生效"，很难定位。
//
// 用 Redis pub/sub 广播"策略变了"，而不是让判定本身改走 Redis：
// 判定路径完全不变，只是策略变更这种低频事件才过一次网络。
type RedisWatcher struct {
	rdb     *redis.Client
	channel string
	logger  *slog.Logger
}

// NewRedisWatcher 构造广播器。channel 为空时使用 DefaultWatchChannel。
func NewRedisWatcher(rdb *redis.Client, channel string, logger *slog.Logger) *RedisWatcher {
	if channel == "" {
		channel = DefaultWatchChannel
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &RedisWatcher{rdb: rdb, channel: channel, logger: logger}
}

// Notify 广播一次策略变更，使其余副本重载。对 nil receiver 安全，
// 调用方（如未装配 Redis 的测试）不必为此专门判空。
//
// 广播失败只记日志、不向上返回错误：策略本身已经落库成功，
// 因为"通知其它副本"这一步失败就让整个写操作报错不划算——
// 顶多是其它副本晚一点感知，而不是数据不一致。
func (w *RedisWatcher) Notify(ctx context.Context) {
	w.NotifyVersion(ctx, 0)
}

// NotifyVersion 广播策略版本。版本为 0 兼容旧调用方，仅表达“有变化”。
func (w *RedisWatcher) NotifyVersion(ctx context.Context, version int64) {
	if err := w.PublishVersion(ctx, version); err != nil && w != nil {
		w.logger.ErrorContext(ctx, "authz: 广播策略变更失败", "error", err, "channel", w.channel)
	}
}

// PublishVersion 广播策略版本。请求路径用 NotifyVersion 忽略投递错误；
// 需要区分成功/失败的调用方可以直接使用本方法。
func (w *RedisWatcher) PublishVersion(ctx context.Context, version int64) error {
	if w == nil || w.rdb == nil {
		return errors.New("authz: policy watcher is not configured")
	}
	payload := "changed"
	if version > 0 {
		payload = strconv.FormatInt(version, 10)
	}
	return w.rdb.Publish(ctx, w.channel, payload).Err()
}

// Watch 订阅频道，每收到一条消息就重载 target 的策略，直到 ctx 被取消。
//
// 含本进程自己发出的广播——重载一次内存策略的成本远低于专门维护一套
// "是不是自己发的"去重逻辑，且策略变更本就是低频操作。
//
// 调用方负责另起 goroutine 运行本方法，并在应用退出时取消 ctx。
func (w *RedisWatcher) Watch(ctx context.Context, target Reloadable) {
	sub := w.rdb.Subscribe(ctx, w.channel)
	defer func() { _ = sub.Close() }()

	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ch:
			if !ok {
				return
			}
			// ctx 取消与消息到达可能同时就绪，select 会随机选中一支。
			// 这里再确认一次：应用正在退出时，没必要为最后一条消息
			// 再去查一次可能已经关闭的数据库连接。
			if ctx.Err() != nil {
				return
			}
			if err := target.ReloadPolicy(ctx); err != nil {
				w.logger.ErrorContext(ctx, "authz: 重载策略失败", "error", err)
			}
		}
	}
}
