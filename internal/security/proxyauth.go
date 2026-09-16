package security

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"sync"
	"time"

	"github.com/masteralanlab/free-proxy/internal/domain"
	"github.com/masteralanlab/free-proxy/internal/store"
)

const (
	// proxySettingsTTL is how long the gateway may reuse one settings read.
	// Changing the proxy credentials restarts the service, so this window only
	// ever exists on the way down.
	proxySettingsTTL = 5 * time.Second

	// credentialTTL bounds how long a verification verdict stands. It can be
	// generous because the cache key covers the stored hash: rotating the
	// password changes the hash, which changes the key, so a stale entry becomes
	// unreachable rather than wrong.
	credentialTTL = 5 * time.Minute

	// credentialCacheMax caps the table, so spraying random passwords costs the
	// attacker memory instead of us.
	credentialCacheMax = 512
)

// ProxyAuthenticator answers the two questions the proxy gateway asks on every
// accepted connection — is authentication configured, and are these credentials
// right — without paying full price for them each time.
//
// Both used to go straight to the database, and the second went straight to
// scrypt. The gateway admits PROXY_MAX_CONNECTIONS clients concurrently (256 by
// default) and a browser opens dozens of connections per page load, so ordinary
// use meant hundreds of five-query settings reads and hundreds of simultaneous
// scrypt derivations at 16 MiB apiece. Caching turns a browsing session into one
// read and one derivation.
type ProxyAuthenticator struct {
	repo  *store.AppSettingsRepository
	creds *credentialCache

	mu      sync.Mutex
	proxy   domain.ProxyServiceSettings
	readAt  time.Time
	readErr error
}

// NewProxyAuthenticator constructs a ProxyAuthenticator over the settings repo.
func NewProxyAuthenticator(repo *store.AppSettingsRepository) *ProxyAuthenticator {
	return &ProxyAuthenticator{repo: repo, creds: newCredentialCache(credentialTTL, credentialCacheMax)}
}

// settings returns the proxy settings, reading through to the database at most
// once per proxySettingsTTL. The lock is held across the read on purpose: it
// collapses a burst of connections into a single query instead of letting each
// one issue its own.
func (a *ProxyAuthenticator) settings() (domain.ProxyServiceSettings, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.readAt.IsZero() && time.Since(a.readAt) < proxySettingsTTL {
		return a.proxy, a.readErr
	}
	all, err := a.repo.Get(context.Background())
	a.proxy, a.readErr, a.readAt = all.Proxy, err, time.Now()
	return a.proxy, a.readErr
}

// Required reports whether the proxy must authenticate its clients. It fails
// closed: a database error leaves authentication on. The in-process health
// credential is verified separately and still works, so monitoring does not read
// the outage as a dead exit.
func (a *ProxyAuthenticator) Required() bool {
	s, err := a.settings()
	return err != nil || s.Username != "" && s.PasswordHash != ""
}

// Authenticate verifies a proxy client's credentials.
func (a *ProxyAuthenticator) Authenticate(username, password string) bool {
	s, err := a.settings()
	if err != nil || s.Username == "" || s.PasswordHash == "" {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(username), []byte(s.Username)) != 1 {
		return false
	}
	return a.creds.verify(password, s.PasswordHash)
}

// credentialCache memoizes scrypt verdicts. Keys are a keyed hash of the
// candidate password and the stored hash under a per-process key, so the table
// holds nothing useful to anyone who reads it out of a core dump.
type credentialCache struct {
	ttl time.Duration
	max int
	key []byte
	// derive is the underlying verification, swapped out in tests to count how
	// often the cache actually reaches for one.
	derive func(password, encodedHash string) bool

	mu      sync.Mutex
	entries map[string]credentialEntry
}

type credentialEntry struct {
	ok        bool
	expiresAt time.Time
	// done is non-nil while a derivation for this key is in flight, and is
	// closed when the verdict lands.
	done chan struct{}
}

func newCredentialCache(ttl time.Duration, max int) *credentialCache {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		// Without a key there is no safe way to index the table, so leave it nil
		// and let every call fall through to scrypt.
		key = nil
	}
	return &credentialCache{ttl: ttl, max: max, key: key, derive: VerifyPassword, entries: map[string]credentialEntry{}}
}

// verify answers VerifyPassword, reusing a recent verdict when there is one.
//
// Negative verdicts are cached too. A client repeating the wrong password is
// exactly the case that must not cost a derivation every time, and the key
// covers the password, so a cached "no" can never mask a later correct one.
func (c *credentialCache) verify(password, encodedHash string) bool {
	if c == nil || c.key == nil {
		return VerifyPassword(password, encodedHash)
	}
	k := c.digest(password, encodedHash)
	for {
		c.mu.Lock()
		entry, found := c.entries[k]
		if found && entry.done != nil {
			// Another connection is already deriving this exact pair. Waiting for
			// it beats starting a second 16 MiB derivation: a burst of connections
			// carrying one client's credentials is the normal case here, not the
			// exceptional one.
			c.mu.Unlock()
			<-entry.done
			continue
		}
		if found && time.Now().Before(entry.expiresAt) {
			c.mu.Unlock()
			return entry.ok
		}
		done := make(chan struct{})
		c.evictLocked(time.Now())
		c.entries[k] = credentialEntry{done: done}
		c.mu.Unlock()

		verdict := c.derive(password, encodedHash)

		c.mu.Lock()
		c.entries[k] = credentialEntry{ok: verdict, expiresAt: time.Now().Add(c.ttl)}
		c.mu.Unlock()
		close(done)
		return verdict
	}
}

// evictLocked keeps the table under its cap. Expired entries go first; if that
// is not enough, arbitrary ones do — Go randomizes map iteration, which is what
// stops a caller from steering which entry it displaces. Entries with a
// derivation in flight are never evicted, so their waiters still find a verdict.
func (c *credentialCache) evictLocked(now time.Time) {
	if len(c.entries) < c.max {
		return
	}
	for k, e := range c.entries {
		if e.done == nil && !now.Before(e.expiresAt) {
			delete(c.entries, k)
		}
	}
	for k, e := range c.entries {
		if len(c.entries) < c.max {
			return
		}
		if e.done == nil {
			delete(c.entries, k)
		}
	}
}

// digest derives the cache key. The password is length-prefixed so that no two
// distinct (password, hash) pairs can concatenate to the same input.
func (c *credentialCache) digest(password, encodedHash string) string {
	mac := hmac.New(sha256.New, c.key)
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(password)))
	_, _ = mac.Write(n[:])
	_, _ = mac.Write([]byte(password))
	_, _ = mac.Write([]byte(encodedHash))
	return string(mac.Sum(nil))
}
