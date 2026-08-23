package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/rueidis"

	"github.com/eagle-go/eagle/app/product/internal/product/domain"
	"github.com/eagle-go/eagle/pkg/platform/config"
)

type productCache interface {
	Get(context.Context, int64) (*domain.Product, bool, error)
	Set(context.Context, *domain.Product) error
	Delete(context.Context, int64) error
}

type RedisProductCache struct {
	client rueidis.Client
	ttl    time.Duration
}

func NewRedisProductCache(config *config.Cache_Redis) (*RedisProductCache, func(), error) {
	client, err := rueidis.NewClient(rueidis.ClientOption{
		InitAddress: []string{config.GetAddress()}, Username: config.GetUsername(), Password: config.GetPassword(),
		SelectDB: int(config.GetDatabase()), ClientName: "eagle-product",
	})
	if err != nil {
		return nil, nil, fmt.Errorf("connect redis: %w", err)
	}
	return &RedisProductCache{client: client, ttl: config.GetTtl().AsDuration()}, client.Close, nil
}

func productCacheKey(id int64) string { return "eagle:product:v1:" + strconv.FormatInt(id, 10) }

func (c *RedisProductCache) Get(ctx context.Context, id int64) (*domain.Product, bool, error) {
	value, err := c.client.Do(ctx, c.client.B().Get().Key(productCacheKey(id)).Build()).ToString()
	if rueidis.IsRedisNil(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("redis get product: %w", err)
	}
	var product domain.Product
	if err := json.Unmarshal([]byte(value), &product); err != nil {
		_ = c.Delete(ctx, id)
		return nil, false, fmt.Errorf("decode cached product: %w", err)
	}
	return &product, true, nil
}

func (c *RedisProductCache) Set(ctx context.Context, product *domain.Product) error {
	value, err := json.Marshal(product)
	if err != nil {
		return fmt.Errorf("encode cached product: %w", err)
	}
	command := c.client.B().Set().Key(productCacheKey(product.ID)).Value(string(value)).Ex(c.ttl).Build()
	if err := c.client.Do(ctx, command).Error(); err != nil {
		return fmt.Errorf("redis set product: %w", err)
	}
	return nil
}

func (c *RedisProductCache) Delete(ctx context.Context, id int64) error {
	if err := c.client.Do(ctx, c.client.B().Del().Key(productCacheKey(id)).Build()).Error(); err != nil {
		return fmt.Errorf("redis delete product: %w", err)
	}
	return nil
}
