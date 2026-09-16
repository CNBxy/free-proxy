package security

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func countingCache(t *testing.T, ttl time.Duration, max int, verdict func(pw, hash string) bool) (*credentialCache, *atomic.Int64) {
	t.Helper()
	c := newCredentialCache(ttl, max)
	var calls atomic.Int64
	c.derive = func(pw, hash string) bool {
		calls.Add(1)
		return verdict(pw, hash)
	}
	return c, &calls
}

// The point of the cache is that a browsing session's worth of connections costs
// one derivation, not one per connection.
func TestCredentialCacheReusesVerdicts(t *testing.T) {
	c, calls := countingCache(t, time.Minute, 64, func(pw, _ string) bool { return pw == "right" })

	for i := 0; i < 50; i++ {
		if !c.verify("right", "stored-hash") {
			t.Fatalf("call %d: correct password rejected", i)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("derivations for one credential: got %d, want 1", got)
	}

	// A wrong password is cached too — repeating it is exactly the case that must
	// not cost a derivation each time.
	for i := 0; i < 50; i++ {
		if c.verify("wrong", "stored-hash") {
			t.Fatalf("call %d: wrong password accepted", i)
		}
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("derivations after adding a wrong password: got %d, want 2", got)
	}
}

// Rotating the password changes the stored hash, which changes the key, so the
// old verdict becomes unreachable rather than wrong.
func TestCredentialCacheKeyCoversStoredHash(t *testing.T) {
	accepted := "old"
	c, calls := countingCache(t, time.Minute, 64, func(pw, _ string) bool { return pw == accepted })

	if !c.verify("old", "hash-v1") {
		t.Fatal("old password rejected against v1 hash")
	}
	accepted = "new"
	if c.verify("old", "hash-v2") {
		t.Fatal("old password accepted against the rotated hash")
	}
	if !c.verify("new", "hash-v2") {
		t.Fatal("new password rejected against the rotated hash")
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("derivations: got %d, want 3", got)
	}
}

func TestCredentialCacheExpiresEntries(t *testing.T) {
	c, calls := countingCache(t, 10*time.Millisecond, 64, func(string, string) bool { return true })

	c.verify("pw", "hash")
	c.verify("pw", "hash")
	if got := calls.Load(); got != 1 {
		t.Fatalf("before expiry: got %d derivations, want 1", got)
	}
	time.Sleep(20 * time.Millisecond)
	c.verify("pw", "hash")
	if got := calls.Load(); got != 2 {
		t.Errorf("after expiry: got %d derivations, want 2", got)
	}
}

// Spraying distinct passwords must not grow the table without bound.
func TestCredentialCacheStaysUnderCap(t *testing.T) {
	const max = 32
	c, _ := countingCache(t, time.Hour, max, func(string, string) bool { return false })

	for i := 0; i < 500; i++ {
		c.verify(string(rune('a'+i%26))+string(rune(i)), "hash")
	}
	c.mu.Lock()
	size := len(c.entries)
	c.mu.Unlock()
	if size > max {
		t.Errorf("cache holds %d entries, cap is %d", size, max)
	}
}

// Concurrent connections carrying the same credentials must collapse onto one
// derivation rather than each starting their own 16 MiB one.
func TestCredentialCacheSingleFlights(t *testing.T) {
	c, calls := countingCache(t, time.Minute, 64, func(string, string) bool {
		time.Sleep(20 * time.Millisecond)
		return true
	})

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !c.verify("pw", "hash") {
				t.Error("verification failed")
			}
		}()
	}
	wg.Wait()
	if got := calls.Load(); got != 1 {
		t.Errorf("concurrent derivations: got %d, want 1", got)
	}
}

func TestAttemptLimiterEnforcesWindow(t *testing.T) {
	l := NewAttemptLimiter(3, time.Minute, 16)
	for i := 0; i < 3; i++ {
		if !l.Allow("1.2.3.4") {
			t.Fatalf("attempt %d was refused inside the limit", i+1)
		}
	}
	if l.Allow("1.2.3.4") {
		t.Error("attempt over the limit was allowed")
	}
	// The limit is per client.
	if !l.Allow("5.6.7.8") {
		t.Error("a different client was refused")
	}
	// Authenticating clears the window, so a person who mistypes twice and then
	// gets it right is not left throttled.
	l.Reset("1.2.3.4")
	if !l.Allow("1.2.3.4") {
		t.Error("client still throttled after Reset")
	}
}

func TestAttemptLimiterWindowRolls(t *testing.T) {
	l := NewAttemptLimiter(1, 10*time.Millisecond, 16)
	if !l.Allow("c") {
		t.Fatal("first attempt refused")
	}
	if l.Allow("c") {
		t.Fatal("second attempt allowed inside the window")
	}
	time.Sleep(20 * time.Millisecond)
	if !l.Allow("c") {
		t.Error("attempt refused after the window elapsed")
	}
}

// A full table of live windows is an attack, and refusing beats letting the
// table grow — but expired windows must be reclaimed first.
func TestAttemptLimiterBoundsItsTable(t *testing.T) {
	l := NewAttemptLimiter(5, 10*time.Millisecond, 8)
	for i := 0; i < 100; i++ {
		l.Allow(string(rune(i)))
	}
	l.mu.Lock()
	size := len(l.buckets)
	l.mu.Unlock()
	if size > 8 {
		t.Fatalf("limiter holds %d buckets, cap is 8", size)
	}
	time.Sleep(20 * time.Millisecond)
	if !l.Allow("fresh-client") {
		t.Error("new client refused after every window had elapsed")
	}
}

// A hash whose parameters would reserve more than maxScryptMemory is refused
// rather than derived: the row is ours, but a corrupt one should not be able to
// turn a login into an OOM.
func TestVerifyPasswordRejectsOversizedParameters(t *testing.T) {
	if VerifyPassword("pw", "scrypt$1073741824$8$1$c2FsdA==$ZGlnZXN0") {
		t.Error("oversized scrypt parameters were accepted")
	}
	if VerifyPassword("pw", "scrypt$0$8$1$c2FsdA==$ZGlnZXN0") {
		t.Error("zero cost parameter was accepted")
	}
}
