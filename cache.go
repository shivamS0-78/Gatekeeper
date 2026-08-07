package main

import (
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
