package resilience

import (
	"sync"
	"time"
)

type State uint8

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF-OPEN"
	default:
		return "UNKNOWN"
	}
}

type CircuitBreakerConfig struct {
	FailureThreshold int
	RecoveryTimeout  time.Duration
}

// CircuitBreaker protects a single upstream from repeated failing requests.
// It is local to one Gatekeeper process; it does not share state across instances.
type CircuitBreaker struct {
	mu sync.Mutex

	name             string
	state            State
	consecutiveFails int
	failureThreshold int
	recoveryTimeout  time.Duration
	openedAt         time.Time
	probeInFlight    bool

	// generation invalidates outcomes from requests started in an older state.
	generation uint64
	now        func() time.Time
}

// Permit represents permission for one request to use this upstream.
// Call exactly one of RecordSuccess, RecordFailure, or Abandon for each permit.
type Permit struct {
	breaker    *CircuitBreaker
	generation uint64
	probe      bool
	once       sync.Once
}

// NewCircuitBreaker creates a circuit breaker for one upstream. Invalid or
// omitted settings are replaced with conservative defaults.
func NewCircuitBreaker(name string, cfg CircuitBreakerConfig) *CircuitBreaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 3
	}
	if cfg.RecoveryTimeout <= 0 {
		cfg.RecoveryTimeout = 10 * time.Second
	}
	if name == "" {
		name = "unknown-upstream"
	}

	return &CircuitBreaker{
		name:             name,
		state:            StateClosed,
		failureThreshold: cfg.FailureThreshold,
		recoveryTimeout:  cfg.RecoveryTimeout,
		now:              time.Now,
	}
}
