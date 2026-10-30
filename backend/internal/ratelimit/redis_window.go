package ratelimit

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisFixedWindow struct {
	rdb      *redis.Client
	limit    int
	size     time.Duration
	timeout  time.Duration
	fallback *FixedWindow
	logOnce  sync.Once
}

func NewRedisFixedWindow(rdb *redis.Client, limit int, size time.Duration) *RedisFixedWindow {
	return &RedisFixedWindow{
		rdb:      rdb,
		limit:    limit,
		size:     size,
		timeout:  50 * time.Millisecond,
		fallback: NewFixedWindow(limit, size, nil),
	}
}

func (r *RedisFixedWindow) Allow(key string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel() // cleanup context after the function ends

	windowStart := time.Now().Truncate(r.size).Unix()

	k := "rl:" + key + ":" + strconv.FormatInt(windowStart, 10)

	pipe := r.rdb.TxPipeline()
	incr := pipe.Incr(ctx, k)
	pipe.Expire(ctx, k, r.size)

	if _, err := pipe.Exec(ctx); err != nil {
		r.logOnce.Do(func() { slog.Warn("rate limiter falling back to memory", "err", err) })
		return r.fallback.Allow(key)
	}

	return incr.Val() <= int64(r.limit)
}
