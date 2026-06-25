package panel

import (
	"strings"
	"sync"
	"time"
)

type loginAttemptLimiter struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	blockFor time.Duration
	attempts map[string]loginAttemptState
}

type loginAttemptState struct {
	Failures     int
	FirstFailure time.Time
	BlockedUntil time.Time
}

func newLoginAttemptLimiter(max int, window time.Duration, blockFor time.Duration) *loginAttemptLimiter {
	if max <= 0 {
		max = 5
	}
	if window <= 0 {
		window = 10 * time.Minute
	}
	if blockFor <= 0 {
		blockFor = 5 * time.Minute
	}
	return &loginAttemptLimiter{
		max:      max,
		window:   window,
		blockFor: blockFor,
		attempts: make(map[string]loginAttemptState),
	}
}

func loginAttemptKey(username string, ip string) string {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" {
		username = "_"
	}
	return strings.TrimSpace(ip) + "\x00" + username
}

func (l *loginAttemptLimiter) blocked(key string, now time.Time) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	state, ok := l.attempts[key]
	if !ok || state.BlockedUntil.IsZero() || !state.BlockedUntil.After(now) {
		return 0, false
	}
	return state.BlockedUntil.Sub(now), true
}

func (l *loginAttemptLimiter) recordFailure(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	state := l.attempts[key]
	if state.FirstFailure.IsZero() || now.Sub(state.FirstFailure) > l.window {
		state = loginAttemptState{FirstFailure: now}
	}
	state.Failures++
	if state.Failures >= l.max {
		state.BlockedUntil = now.Add(l.blockFor)
	}
	l.attempts[key] = state
}

func (l *loginAttemptLimiter) recordSuccess(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}
