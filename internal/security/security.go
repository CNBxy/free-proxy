// Package security implements admin credential storage (scrypt), the session
// manager, and the auth service. The scrypt hash format is byte-compatible with
// the former Python implementation, so existing web-config.json hashes verify.
package security

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/masteralanlab/free-proxy/internal/config"
	"github.com/masteralanlab/free-proxy/internal/store"
	"golang.org/x/crypto/scrypt"
)

// maxScryptMemory bounds the working set a single derivation may ask for.
// scrypt sizes that set as 128*r*N bytes — 16 MiB at the parameters below — and
// the parameters are read back out of the stored hash. They are ours to begin
// with, but a corrupt row should not be able to turn a login into an OOM.
const maxScryptMemory = 64 << 20

// scryptGate bounds how many derivations run at once.
//
// Every caller here is driven by something external: a proxy client opening a
// connection, a browser posting the login form. Without a bound the peak is set
// by whoever is knocking rather than by what the host has, and each derivation
// in flight holds 16 MiB. The proxy gateway alone admits PROXY_MAX_CONNECTIONS
// clients concurrently — 256 by default, or 4 GiB of scratch memory and every
// core pinned, from one browser opening one page.
var scryptGate = make(chan struct{}, scryptConcurrency())

func scryptConcurrency() int {
	n := runtime.NumCPU()
	if n > 4 {
		n = 4
	}
	if n < 1 {
		n = 1
	}
	return n
}

// deriveScrypt runs scrypt.Key while holding a slot in scryptGate. Callers queue
// rather than fail: waiting costs a parked goroutine, admitting them all costs
// 16 MiB each.
func deriveScrypt(pw, salt []byte, n, r, p, keyLen int) ([]byte, error) {
	scryptGate <- struct{}{}
	defer func() { <-scryptGate }()
	return scrypt.Key(pw, salt, n, r, p, keyLen)
}

// HashPassword returns a scrypt hash in the format scrypt$16384$8$1$salt$digest.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	dk, err := deriveScrypt([]byte(pw), salt, 1<<14, 8, 1, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("scrypt$16384$8$1$%s$%s",
		base64.URLEncoding.EncodeToString(salt),
		base64.URLEncoding.EncodeToString(dk)), nil
}

// VerifyPassword checks pw against an encoded scrypt hash.
func VerifyPassword(pw, encoded string) bool {
	p := strings.SplitN(encoded, "$", 6)
	if len(p) != 6 || p[0] != "scrypt" {
		return false
	}
	n, err1 := strconv.Atoi(p[1])
	r, err2 := strconv.Atoi(p[2])
	pp, err3 := strconv.Atoi(p[3])
	if err1 != nil || err2 != nil || err3 != nil {
		return false
	}
	// scrypt makes two large allocations: 128*r*N for the mixing buffer and
	// 128*r*p for the blocks. Its own guard only rejects r*p >= 1<<30, which
	// still admits a p asking for tens of gigabytes, so bound both.
	if n <= 0 || r <= 0 || pp <= 0 ||
		128*int64(r)*int64(n) > maxScryptMemory || 128*int64(r)*int64(pp) > maxScryptMemory {
		return false
	}
	salt, err := base64.URLEncoding.DecodeString(p[4])
	if err != nil {
		return false
	}
	want, err := base64.URLEncoding.DecodeString(p[5])
	if err != nil {
		return false
	}
	got, err := deriveScrypt([]byte(pw), salt, n, r, pp, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

const credentialAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// RandomCredential returns a random string whose first char is a letter and that
// contains at least one lower, one upper, and one digit.
func RandomCredential(length int) string {
	if length < 4 {
		length = 12
	}
	for {
		b := make([]byte, length)
		if _, err := rand.Read(b); err != nil {
			continue
		}
		out := make([]byte, length)
		for i, v := range b {
			out[i] = credentialAlphabet[int(v)%len(credentialAlphabet)]
		}
		s := string(out)
		if unicode.IsLetter(rune(s[0])) && strings.IndexFunc(s, unicode.IsLower) >= 0 &&
			strings.IndexFunc(s, unicode.IsUpper) >= 0 && strings.IndexFunc(s, unicode.IsDigit) >= 0 {
			return s
		}
	}
}

// AdminConfig is the database-backed admin/listener configuration. Host values
// are fixed listener constants; the ports and exposure flags are persisted.
type AdminConfig struct {
	Username     string
	PasswordHash string
	// Password is the recoverable copy of the admin password so operators can
	// read it back with `free-proxy credentials` instead of rotating (which
	// restarts the service and drops the live tunnel). PasswordHash stays
	// authoritative for verification; Password is only ever printed locally.
	Password            string
	SecretPath          string
	Host                string
	Port                int
	ProxyHost           string
	ProxyPort           int
	WebExternalAccess   *bool
	ProxyExternalAccess *bool
}

func (c AdminConfig) WebExternalAllowed() bool {
	return c.WebExternalAccess == nil || *c.WebExternalAccess
}

func (c AdminConfig) ProxyExternalAllowed() bool {
	return c.ProxyExternalAccess != nil && *c.ProxyExternalAccess
}

// AdminConfigStore persists management credentials in SQLite. web-config.json
// is read only for a one-time migration and is removed after the transaction.
type AdminConfigStore struct {
	cfg  *config.Config
	repo *store.AppSettingsRepository

	mu     sync.RWMutex
	config AdminConfig
}

// NewAdminConfigStore loads database settings, migrates legacy files, or creates
// random first-install credentials.
func NewAdminConfigStore(cfg *config.Config, repo *store.AppSettingsRepository) (*AdminConfigStore, error) {
	s := &AdminConfigStore{cfg: cfg, repo: repo}
	if err := s.loadOrCreate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *AdminConfigStore) Config() AdminConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

func (s *AdminConfigStore) Update(c AdminConfig) error {
	all, err := s.repo.Get(context.Background())
	if err != nil {
		return err
	}
	all.Admin.Username = c.Username
	all.Admin.PasswordHash = c.PasswordHash
	all.Admin.Password = c.Password
	all.Admin.SecretPath = c.SecretPath
	all.Admin.WebPort = c.Port
	all.Admin.WebExternalAccess = c.WebExternalAllowed()
	all.Proxy.Port = c.ProxyPort
	all.Proxy.ExternalAccess = c.ProxyExternalAllowed()
	if err := s.repo.UpdateAdmin(context.Background(), all.Admin); err != nil {
		return err
	}
	if err := s.repo.UpdateProxy(context.Background(), all.Proxy); err != nil {
		return err
	}
	s.mu.Lock()
	s.config = c
	s.mu.Unlock()
	return nil
}

// Rotate replaces the username, management path, and password at once.
func (s *AdminConfigStore) Rotate() (AdminConfig, string, error) {
	c := s.Config()
	c.Username, c.SecretPath = RandomCredential(12), RandomCredential(12)
	return s.setNewPassword(c)
}

// ResetPassword issues a new random password and keeps the username and
// management path, so existing bookmarks and the login name still work.
func (s *AdminConfigStore) ResetPassword() (AdminConfig, string, error) {
	return s.setNewPassword(s.Config())
}

func (s *AdminConfigStore) setNewPassword(c AdminConfig) (AdminConfig, string, error) {
	password := RandomCredential(12)
	hash, err := HashPassword(password)
	if err != nil {
		return AdminConfig{}, "", err
	}
	c.PasswordHash, c.Password = hash, password
	if err := s.Update(c); err != nil {
		return AdminConfig{}, "", err
	}
	return c, password, nil
}

func (s *AdminConfigStore) SetExternalAccess(web, proxy bool) error {
	c := s.Config()
	c.WebExternalAccess, c.ProxyExternalAccess = &web, &proxy
	return s.Update(c)
}

func (s *AdminConfigStore) loadOrCreate() error {
	ctx := context.Background()
	all, err := s.repo.Get(ctx)
	if err != nil {
		return err
	}
	legacyPath := filepath.Join(s.cfg.DataDir, "web-config.json")
	bootstrapPath := filepath.Join(s.cfg.DataDir, "initial-admin-password")
	legacy, legacyOK := readLegacyAdmin(legacyPath)
	if all.Admin.PasswordHash == "" && legacyOK {
		if legacy.PasswordHash == "" && legacy.PlaintextPassword != "" {
			legacy.PasswordHash, err = HashPassword(legacy.PlaintextPassword)
			if err != nil {
				return err
			}
		}
		all.Admin.Username = firstNonEmpty(legacy.Username, RandomCredential(12))
		all.Admin.PasswordHash = legacy.PasswordHash
		all.Admin.Password = legacy.PlaintextPassword
		all.Admin.SecretPath = firstNonEmpty(legacy.SecretPath, RandomCredential(12))
		all.Admin.WebPort = legacy.Port
		if all.Admin.WebPort == 0 || all.Admin.WebPort == 8787 {
			all.Admin.WebPort = 39527
		}
		all.Admin.WebExternalAccess = legacy.WebExternalAllowed()
		if legacy.ProxyPort > 0 {
			all.Proxy.Port = legacy.ProxyPort
		}
		all.Proxy.ExternalAccess = legacy.ProxyExternalAllowed()
		if err = s.repo.UpdateAdmin(ctx, all.Admin); err != nil {
			return err
		}
		if err = s.repo.UpdateProxy(ctx, all.Proxy); err != nil {
			return err
		}
	}
	if all.Admin.PasswordHash == "" {
		password := firstNonEmpty(s.cfg.AdminPassword, RandomCredential(12))
		hash, hashErr := HashPassword(password)
		if hashErr != nil {
			return hashErr
		}
		all.Admin.Username = firstNonEmpty(s.cfg.AdminUsername, RandomCredential(12))
		all.Admin.PasswordHash = hash
		all.Admin.Password = password
		all.Admin.SecretPath = firstNonEmpty(s.cfg.AdminSecretPath, RandomCredential(12))
		if all.Admin.WebPort == 0 || all.Admin.WebPort == 8787 {
			all.Admin.WebPort = 39527
		}
		if err = s.repo.UpdateAdmin(ctx, all.Admin); err != nil {
			return err
		}
	}
	// An install that predates recoverable storage may still hold its password in
	// plain sight elsewhere: the one-time file from its own first install, or the
	// operator's own FREE_PROXY_ADMIN_PASSWORD. Recovering it there spares that
	// install the reset `free-proxy install` would otherwise perform. Each
	// candidate is adopted only if it verifies — anything else is stale.
	if all.Admin.Password == "" {
		candidates := []string{s.cfg.AdminPassword}
		if data, readErr := os.ReadFile(bootstrapPath); readErr == nil {
			candidates = append(candidates, strings.TrimSpace(string(data)))
		}
		for _, pw := range candidates {
			if pw == "" || !VerifyPassword(pw, all.Admin.PasswordHash) {
				continue
			}
			all.Admin.Password = pw
			if err = s.repo.UpdateAdmin(ctx, all.Admin); err != nil {
				return err
			}
			break
		}
	}
	_ = os.Remove(legacyPath)
	_ = os.Remove(bootstrapPath)
	web, proxy := all.Admin.WebExternalAccess, all.Proxy.ExternalAccess
	s.config = AdminConfig{
		Username: all.Admin.Username, PasswordHash: all.Admin.PasswordHash,
		Password:   all.Admin.Password,
		SecretPath: all.Admin.SecretPath, Host: "0.0.0.0", Port: all.Admin.WebPort,
		ProxyHost: "0.0.0.0", ProxyPort: all.Proxy.Port,
		WebExternalAccess: &web, ProxyExternalAccess: &proxy,
	}
	return nil
}

type legacyAdminConfig struct {
	Username            string `json:"username"`
	PasswordHash        string `json:"password_hash"`
	PlaintextPassword   string `json:"password"`
	SecretPath          string `json:"secret_path"`
	Port                int    `json:"port"`
	ProxyPort           int    `json:"proxy_port"`
	WebExternalAccess   *bool  `json:"web_external_access"`
	ProxyExternalAccess *bool  `json:"proxy_external_access"`
}

func (c legacyAdminConfig) WebExternalAllowed() bool {
	return c.WebExternalAccess == nil || *c.WebExternalAccess
}
func (c legacyAdminConfig) ProxyExternalAllowed() bool {
	return c.ProxyExternalAccess != nil && *c.ProxyExternalAccess
}

func readLegacyAdmin(path string) (legacyAdminConfig, bool) {
	var c legacyAdminConfig
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &c) != nil {
		return c, false
	}
	return c, c.SecretPath != "" || c.PasswordHash != "" || c.PlaintextPassword != ""
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// SessionManager stores active session tokens in memory.
type SessionManager struct {
	ttl      time.Duration
	mu       sync.Mutex
	sessions map[string]time.Time
}

// NewSessionManager creates a SessionManager.
func NewSessionManager(ttl time.Duration) *SessionManager {
	return &SessionManager{ttl: ttl, sessions: map[string]time.Time{}}
}

// Create issues a new session token.
func (m *SessionManager) Create() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := fmt.Sprintf("%x", b)
	now := time.Now()
	m.mu.Lock()
	// Expired tokens are otherwise only dropped when someone presents them, and
	// nobody presents a token they have stopped using. With a 30-day TTL that
	// left every session ever issued in the map for the life of the process.
	if len(m.sessions) >= sessionSweepThreshold {
		for t, exp := range m.sessions {
			if now.After(exp) {
				delete(m.sessions, t)
			}
		}
	}
	m.sessions[token] = now.Add(m.ttl)
	m.mu.Unlock()
	return token, nil
}

// sessionSweepThreshold is the size at which Create pays for a sweep of expired
// tokens. Below it the map is small enough not to be worth walking.
const sessionSweepThreshold = 256

// Valid reports whether a token is present and unexpired.
func (m *SessionManager) Valid(token string) bool {
	if token == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	exp, ok := m.sessions[token]
	if !ok || time.Now().After(exp) {
		delete(m.sessions, token)
		return false
	}
	return true
}

// Remove drops a token.
func (m *SessionManager) Remove(token string) {
	if token == "" {
		return
	}
	m.mu.Lock()
	delete(m.sessions, token)
	m.mu.Unlock()
}

// Clear drops all tokens.
func (m *SessionManager) Clear() {
	m.mu.Lock()
	m.sessions = map[string]time.Time{}
	m.mu.Unlock()
}

// AuthService verifies credentials and holds the store + sessions.
type AuthService struct {
	Cfg      *config.Config
	Store    *AdminConfigStore
	Sessions *SessionManager
	// Logins throttles password verification per client. Verify is an scrypt
	// derivation behind an endpoint that takes unauthenticated requests.
	Logins *AttemptLimiter
}

// NewAuthService constructs an AuthService.
func NewAuthService(cfg *config.Config, store *AdminConfigStore, sessions *SessionManager) *AuthService {
	return &AuthService{Cfg: cfg, Store: store, Sessions: sessions, Logins: NewLoginLimiter()}
}

// Verify checks a username/password against the stored config.
func (a *AuthService) Verify(username, password string) bool {
	c := a.Store.Config()
	return subtle.ConstantTimeCompare([]byte(username), []byte(c.Username)) == 1 &&
		VerifyPassword(password, c.PasswordHash)
}
