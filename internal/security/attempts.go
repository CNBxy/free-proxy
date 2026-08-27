package security

import (
	"sync"
	"time"
)

const (
	// LoginAttemptLimit and LoginAttemptWindow bound how often one client may ask
	// the login endpoint to verify a password. Each verification is an scrypt
	// derivation: ~16 MiB of working memory and tens of milliseconds of CPU,
	// against an endpoint that takes an unauthenticated request. The scrypt gate
	// caps how much of that runs at once; this caps how much is asked for.
	//
	// The limit is generous enough that a person mistyping their password never
	// meets it.
	LoginAttemptLimit  = 10
	LoginAttemptWindow = time.Minute

	// loginAttemptMaxKeys bounds the table itself, so tracking the attempts
	// cannot become its own memory leak.
	loginAttemptMaxKeys = 4096
)

// AttemptLimiter is a fixed-window rate limiter keyed by client.
type AttemptLimiter struct {
	limit   int
	window  time.Duration
	maxKeys int

	mu      sync.Mutex
	buckets map[string]*attemptBucket
}

type attemptBucket struct {
	count       int
	windowStart time.Time
}

// NewAttemptLimiter constructs a limiter allowing limit events per window per key.
func NewAttemptLimiter(limit int, window time.Duration, maxKeys int) *AttemptLimiter {
	return &AttemptLimiter{limit: limit, window: window, maxKeys: maxKeys, buckets: map[string]*attemptBucket{}}
}

// NewLoginLimiter constructs the limiter used by the login endpoint.
func NewLoginLimiter() *AttemptLimiter {
	return NewAttemptLimiter(LoginAttemptLimit, LoginAttemptWindow, loginAttemptMaxKeys)
}

// Allow records an attempt for key and reports whether it may proceed.
//
// When the table is full of live windows the answer is no, even for a key that
// has never been seen. That is deliberate: filling it takes thousands of
// distinct source addresses, which is an attack, and under an attack refusing
// logins beats letting the table — and the derivations behind it — grow without
// bound. The window is a minute, so it clears itself.
func (l *AttemptLimiter) Allow(key string) bool {
	if l == nil || l.limit <= 0 {
		return true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	if b, ok := l.buckets[key]; ok {
		if now.Sub(b.windowStart) >= l.window {
			b.count, b.windowStart = 1, now
			return true
		}
		if b.count >= l.limit {
			return false
		}
		b.count++
		return true
	}

	if len(l.buckets) >= l.maxKeys {
		l.purgeLocked(now)
		if len(l.buckets) >= l.maxKeys {
			return false
		}
	}
	l.buckets[key] = &attemptBucket{count: 1, windowStart: now}
	return true
}

// Reset drops a key's window, so a client that authenticates successfully is not
// held to the failures that preceded it.
func (l *AttemptLimiter) Reset(key string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	delete(l.buckets, key)
	l.mu.Unlock()
}

// purgeLocked drops buckets whose window has elapsed.
func (l *AttemptLimiter) purgeLocked(now time.Time) {
	for k, b := range l.buckets {
		if now.Sub(b.windowStart) >= l.window {
			delete(l.buckets, k)
		}
	}
}
