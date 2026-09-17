package ratelimiter

import (
	"context"
	"database/sql"
	"log"
	"net/http"

	"github.com/redis/go-redis/v9"
)

func ratelimiterSetHeaders(w http.ResponseWriter, rule Rule, remaining, resetTime int64) {
	SetRateLimitHeaders(w, int64(rule.Limit), remaining, resetTime)
}

func RateLimit(db *sql.DB, rdb *redis.Client, ruleCache *RuleCache, next http.Handler) http.Handler {
	limiter := NewRateLimiter(rdb)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()

		userID := r.Header.Get("X-User-ID")
		if userID == "" {
			userID = r.RemoteAddr
		}
		rule, err := ruleCache.GetRule(db, userID)
		if err != nil {
			log.Printf("[DB error] error : %v", err)
			next.ServeHTTP(w, r)
			return
		}

		allowed, remaining, resetTime, err := limiter.AllowSlidingWindow(ctx, userID, rule.Limit, rule.Window)
		if err != nil {
			log.Printf("[Reddis error] error : %v", err)
			next.ServeHTTP(w, r)
			return
		}

		//seting custom headers to http response packet about rate-limting info
		ratelimiterSetHeaders(w, rule, remaining, resetTime)

		if !allowed {
			WriteDenied(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}
