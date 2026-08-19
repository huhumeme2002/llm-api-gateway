package router

import (
	"sync"
	"time"

	"llmgw/internal/config"
)

type State string

const (
	Closed   State = "CLOSED"
	Open     State = "OPEN"
	HalfOpen State = "HALF_OPEN"
)

type breaker struct {
	cfg      config.CircuitBreakerConfig
	mu       sync.Mutex
	state    State
	fails    int
	openedAt time.Time
	halfProbes int
}

func newBreaker(cfg config.CircuitBreakerConfig) *breaker {
	return &breaker{cfg: cfg, state: Closed}
}

func (b *breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case Open:
		if time.Since(b.openedAt) >= b.cfg.Cooldown {
			b.state = HalfOpen
			b.halfProbes = 0
			return true
		}
		return false
	case HalfOpen:
		if b.halfProbes >= b.cfg.HalfOpenMax {
			return false
		}
		b.halfProbes++
		return true
	default:
		return true
	}
}

func (b *breaker) Success() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fails = 0
	b.state = Closed
	b.halfProbes = 0
}

func (b *breaker) Failure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fails++
	if b.state == HalfOpen || b.fails >= b.cfg.Failures {
		b.state = Open
		b.openedAt = time.Now()
	}
}

func (b *breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == Open && time.Since(b.openedAt) >= b.cfg.Cooldown {
		return HalfOpen
	}
	return b.state
}
