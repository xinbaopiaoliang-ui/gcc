package limiter

import (
	"context"
	"sync"
	"time"
)

type ByteLimiter struct {
	rateBytesPerSecond float64
	burst              float64

	mu     sync.Mutex
	tokens float64
	last   time.Time
}

func NewByteLimiter(mbps int) *ByteLimiter {
	if mbps <= 0 {
		return nil
	}
	rate := float64(mbps) * 1000 * 1000 / 8
	return &ByteLimiter{
		rateBytesPerSecond: rate,
		burst:              rate,
		tokens:             rate,
		last:               time.Now(),
	}
}

func (l *ByteLimiter) Allow(n int) bool {
	if l == nil || n <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refillLocked()
	if l.tokens < float64(n) {
		return false
	}
	l.tokens -= float64(n)
	return true
}

func (l *ByteLimiter) Wait(ctx context.Context, n int) error {
	if l == nil || n <= 0 {
		return nil
	}
	need := float64(n)
	for {
		l.mu.Lock()
		l.refillLocked()
		if l.tokens >= need {
			l.tokens -= need
			l.mu.Unlock()
			return nil
		}
		missing := need - l.tokens
		wait := time.Duration(missing / l.rateBytesPerSecond * float64(time.Second))
		if wait < time.Millisecond {
			wait = time.Millisecond
		}
		l.mu.Unlock()

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (l *ByteLimiter) refillLocked() {
	now := time.Now()
	elapsed := now.Sub(l.last).Seconds()
	if elapsed <= 0 {
		return
	}
	l.tokens += elapsed * l.rateBytesPerSecond
	if l.tokens > l.burst {
		l.tokens = l.burst
	}
	l.last = now
}
