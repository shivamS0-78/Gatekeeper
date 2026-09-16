package ratelimiter

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RateLimiter struct {
	rdb *redis.Client
}

func NewRateLimiter(rdb *redis.Client) *RateLimiter {
	return &RateLimiter{rdb: rdb}
}

func (rl *RateLimiter) AllowSlidingWindow(ctx context.Context, userId string, limit int, window time.Duration) (bool, int64, int64, error) {
	key := fmt.Sprintf("ratelimit:sliding:%s", userId)
	now := time.Now()
	clearBefore := now.Add(-window) //generating the timestamps of expired records

	pipe := rl.rdb.Pipeline() // creating pipeline to batch multiple commands

	pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", clearBefore.UnixNano())) //removing expired timestamps
	pipe.ZAdd(ctx, key, redis.Z{
		Score:  float64(now.UnixNano()),
		Member: now.UnixNano(),
	}) // adding current timestamp

	countReq := pipe.ZCard(ctx, key)
	pipe.Expire(ctx, key, window)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, 0, 0, fmt.Errorf("failed to execute pipeline : %w", err)
	}

	currentCOunt := countReq.Val()
	remaining := int64(limit) - currentCOunt

	resetTime := now.Add(window).Unix()

	if currentCOunt >= int64(limit) {
		return false, remaining, resetTime, nil
	}

	return true, remaining, resetTime, nil
}
