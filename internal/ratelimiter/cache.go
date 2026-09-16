package ratelimiter

import (
	"database/sql"
	"sync"
	"time"
)

type Rule struct {
	Limit  int
	Window time.Duration
}

type CachedRule struct {
	Rule      Rule
	ExpiresAt time.Time
}

type RuleCache struct {
	mu    sync.RWMutex
	store map[string]CachedRule
}

func NewRuleCache() *RuleCache {
	return &RuleCache{
		store: make(map[string]CachedRule),
	}
}

// fetching rules from cache if present
func (c *RuleCache) GetRule(db *sql.DB, userID string) (Rule, error) {
	c.mu.RLock()
	cached, found := c.store[userID]
	if found && time.Now().Before(cached.ExpiresAt) {
		c.mu.RUnlock()
		return cached.Rule, nil
	}
	c.mu.RUnlock()

	rule := Rule{Limit: 5, Window: 10 * time.Second} // for now using harcoded fallback rule instead querying PG for now

	c.mu.Lock()
	c.store[userID] = CachedRule{
		Rule:      rule,
		ExpiresAt: time.Now().Add(30 * time.Second),
	}
	c.mu.Unlock()

	return rule, nil
}
