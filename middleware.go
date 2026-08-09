package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"

	"github.com/redis/go-redis/v9"
)

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
		if !allowed {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(fmt.Sprintf(`{"error": "rate limit exceeded . Try again later`)))
			return
		}
		next.ServeHTTP(w, r)
	})
}
