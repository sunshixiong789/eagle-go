package authn

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RevocationKeyPrefix 是黑名单键前缀。
const RevocationKeyPrefix = "eagle:revoked:"

// RedisRevocations 用 Redis 维护已撤销 token 的 jti 黑名单。
//
// 之所以是黑名单而不是白名单：access token 是自包含的 JWT，
// 正常路径下资源服务器本地验签即可，不必访问任何中心化存储。
// 只有"提前作废"这种例外情况才需要额外记录，量级远小于活跃 token 总数。
//
// 每条记录的 TTL 设为 token 的剩余寿命——token 自然过期后
// 签名校验本身就会拒绝它，黑名单记录再留着纯属浪费内存。
type RedisRevocations struct {
	cli *redis.Client
}

// NewRedisRevocations 构造基于 Redis 的撤销存储。
func NewRedisRevocations(cli *redis.Client) *RedisRevocations {
	return &RedisRevocations{cli: cli}
}

// IsRevoked 查询 jti 是否已被撤销。
func (r *RedisRevocations) IsRevoked(ctx context.Context, jti string) (bool, error) {
	n, err := r.cli.Exists(ctx, RevocationKeyPrefix+jti).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return false, nil
		}
		return false, fmt.Errorf("query revocation of %q: %w", jti, err)
	}
	return n > 0, nil
}

// Revoke 把 jti 加入黑名单，保留到 expiresAt 为止。
// expiresAt 已过去则直接跳过——那种 token 靠 exp 校验就会被拒。
func (r *RedisRevocations) Revoke(ctx context.Context, jti string, expiresAt time.Time) error {
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return nil
	}
	if err := r.cli.Set(ctx, RevocationKeyPrefix+jti, "1", ttl).Err(); err != nil {
		return fmt.Errorf("revoke %q: %w", jti, err)
	}
	return nil
}
