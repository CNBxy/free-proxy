package security

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/masteralanlab/free-proxy/internal/config"
	"github.com/masteralanlab/free-proxy/internal/store"
	"golang.org/x/crypto/scrypt"
)

// TestScryptRFC7914Vector confirms the scrypt KDF matches the RFC 7914 vector,
// which is the same standard the former Python hashlib.scrypt uses — so hashes
// produced by either implementation verify in the other.
func TestScryptRFC7914Vector(t *testing.T) {
	got, err := scrypt.Key([]byte(""), []byte(""), 16, 1, 1, 64)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x77, 0xd6, 0x57, 0x62, 0x38, 0x65, 0x7b, 0x20, 0x3b, 0x19, 0xca, 0x42, 0xc1, 0x8a, 0x04, 0x97,
		0xf1, 0x6b, 0x48, 0x44, 0xe3, 0x07, 0x4a, 0xe8, 0xdf, 0xdf, 0xfa, 0x3f, 0xed, 0xe2, 0x14, 0x42,
		0xfc, 0xd0, 0x06, 0x9d, 0xed, 0x09, 0x48, 0xf8, 0x32, 0x6a, 0x75, 0x3a, 0x0f, 0xc8, 0x1f, 0x17,
		0xe8, 0xd3, 0xe0, 0xfb, 0x2e, 0x0d, 0x36, 0x28, 0xcf, 0x35, 0xe2, 0x0c, 0x38, 0xd1, 0x89, 0x06,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("scrypt RFC 7914 vector mismatch")
	}
}

func TestHashVerifyRoundTrip(t *testing.T) {
	hash, err := HashPassword("Sup3rSecret!")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "scrypt$16384$8$1$") {
		t.Fatalf("unexpected format: %s", hash)
	}
	if !VerifyPassword("Sup3rSecret!", hash) {
		t.Fatal("correct password failed to verify")
	}
	if VerifyPassword("wrong", hash) {
		t.Fatal("wrong password verified")
	}
	if VerifyPassword("Sup3rSecret!", "scrypt$16384$8$1$bad$data") {
		t.Fatal("malformed hash verified")
	}
	if VerifyPassword("x", "not-a-hash") {
		t.Fatal("non-hash verified")
	}
}

// The cost parameters are read back out of the stored hash, so a corrupt or
// tampered row gets to choose how much memory a login allocates. Every case
// here passes scrypt's own validation — it only rejects r*p >= 1<<30 — and
// would be served if VerifyPassword did not bound the sizes itself.
func TestVerifyRejectsOversizedCostParameters(t *testing.T) {
	// salt and digest are well-formed so the parameters are the only thing
	// standing between the call and the allocation.
	const tail = "$c2FsdHNhbHQ=$ZGlnZXN0ZGlnZXN0"
	cases := map[string]string{
		"N sizes the mixing buffer": "scrypt$67108864$8$1" + tail,      // 128*8*2^26 = 64 GiB
		"p sizes the block buffer":  "scrypt$16384$1$536870911" + tail, // 128*1*p  = 64 GiB
		"r multiplies both":         "scrypt$16384$65536$1" + tail,     // 128*65536*16384
	}
	for name, hash := range cases {
		if VerifyPassword("anything", hash) {
			t.Errorf("%s: verified instead of being rejected", name)
		}
	}

	// The shipped parameters must still be accepted, or the bound is too tight.
	live, err := HashPassword("Sup3rSecret!")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("Sup3rSecret!", live) {
		t.Fatal("the parameters this package writes were rejected by its own bound")
	}
}

func TestRandomCredential(t *testing.T) {
	for range 50 {
		c := RandomCredential(12)
		if len(c) != 12 {
			t.Fatalf("length = %d", len(c))
		}
		if !unicode.IsLetter(rune(c[0])) {
			t.Fatalf("first char not a letter: %q", c)
		}
		if strings.IndexFunc(c, unicode.IsLower) < 0 || strings.IndexFunc(c, unicode.IsUpper) < 0 || strings.IndexFunc(c, unicode.IsDigit) < 0 {
			t.Fatalf("missing character class: %q", c)
		}
	}
}

func TestAdminConfigPersistsAcrossReloads(t *testing.T) {
	cfg := &config.Config{
		DataDir: filepath.Join(t.TempDir(), "data"),
		WebHost: "0.0.0.0", WebPort: 39527,
		ProxyHost: "0.0.0.0", ProxyPort: 9527,
	}
	db, err := store.Open("file:" + filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repos := store.NewRepos(db)
	first, err := NewAdminConfigStore(cfg, repos.App)
	if err != nil {
		t.Fatal(err)
	}
	c := first.Config()
	c.Username = "fixed-user"
	c.SecretPath = "fixedPath123"
	c.PasswordHash, err = HashPassword("fixed-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Update(c); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewAdminConfigStore(cfg, repos.App)
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.Config()
	if got.Username != c.Username || got.SecretPath != c.SecretPath || got.PasswordHash != c.PasswordHash {
		t.Fatalf("credentials changed after reload: got %+v want %+v", got, c)
	}
	if !VerifyPassword("fixed-password", got.PasswordHash) {
		t.Fatal("persisted password hash no longer verifies")
	}
}

// The whole point of storing the password: an operator who forgot it must be
// able to read it back later instead of rotating (which restarts the service).
func TestGeneratedPasswordIsRecoverableAfterReload(t *testing.T) {
	cfg, repos := testAdminEnv(t)
	first, err := NewAdminConfigStore(cfg, repos.App)
	if err != nil {
		t.Fatal(err)
	}
	created := first.Config()
	if created.Password == "" {
		t.Fatal("first install did not store a recoverable password")
	}
	if !VerifyPassword(created.Password, created.PasswordHash) {
		t.Fatal("stored password does not match the stored hash")
	}

	reloaded, err := NewAdminConfigStore(cfg, repos.App)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Config().Password; got != created.Password {
		t.Fatalf("password after reload = %q, want %q", got, created.Password)
	}
}

// The upgrade path for a hash-only install: `install` resets the password once.
// The management path and username must survive, or existing bookmarks break.
func TestResetPasswordKeepsPathAndUsername(t *testing.T) {
	cfg, repos := testAdminEnv(t)
	admin, err := NewAdminConfigStore(cfg, repos.App)
	if err != nil {
		t.Fatal(err)
	}
	before := admin.Config()

	after, password, err := admin.ResetPassword()
	if err != nil {
		t.Fatal(err)
	}
	if after.Username != before.Username || after.SecretPath != before.SecretPath {
		t.Fatalf("reset changed identity: %+v -> %+v", before, after)
	}
	if password == before.Password || password == "" {
		t.Fatalf("reset did not issue a new password: %q", password)
	}
	if !VerifyPassword(password, after.PasswordHash) {
		t.Fatal("new password does not verify against the new hash")
	}
}

// Installs created before the password was stored have a hash only. A password
// still readable elsewhere — the one-time file, or FREE_PROXY_ADMIN_PASSWORD —
// is adopted when it verifies against that hash, and ignored when it does not
// (which means the password was changed since and the copy is stale).
func TestHashOnlyInstallAdoptsAVerifyingPassword(t *testing.T) {
	for _, tc := range []struct {
		name, file, env, want string
	}{
		{name: "matching file is adopted", file: "old-password", want: "old-password"},
		{name: "stale file is ignored", file: "some-other-password", want: ""},
		{name: "matching env password is adopted", env: "old-password", want: "old-password"},
		{name: "stale env password is ignored", env: "some-other-password", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, repos := testAdminEnv(t)
			cfg.AdminPassword = tc.env
			if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
				t.Fatal(err)
			}
			hash, err := HashPassword("old-password")
			if err != nil {
				t.Fatal(err)
			}
			settings, err := repos.App.Get(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			settings.Admin.Username, settings.Admin.PasswordHash, settings.Admin.SecretPath = "u", hash, "p"
			if err := repos.App.UpdateAdmin(context.Background(), settings.Admin); err != nil {
				t.Fatal(err)
			}
			bootstrapPath := filepath.Join(cfg.DataDir, "initial-admin-password")
			if err := os.WriteFile(bootstrapPath, []byte(tc.file+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			admin, err := NewAdminConfigStore(cfg, repos.App)
			if err != nil {
				t.Fatal(err)
			}
			if got := admin.Config().Password; got != tc.want {
				t.Fatalf("recovered password = %q, want %q", got, tc.want)
			}
			if _, err := os.Stat(bootstrapPath); !os.IsNotExist(err) {
				t.Fatal("initial-admin-password was not removed")
			}
		})
	}
}

func testAdminEnv(t *testing.T) (*config.Config, *store.Repos) {
	t.Helper()
	cfg := &config.Config{
		DataDir: filepath.Join(t.TempDir(), "data"),
		WebHost: "0.0.0.0", WebPort: 39527,
		ProxyHost: "0.0.0.0", ProxyPort: 9527,
	}
	db, err := store.Open("file:" + filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return cfg, store.NewRepos(db)
}

func TestLegacyWebConfigMigratesToDatabaseAndIsRemoved(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hash, err := HashPassword("legacy-password")
	if err != nil {
		t.Fatal(err)
	}
	legacy := `{"username":"legacy-user","password_hash":"` + hash + `","secret_path":"legacyPath1","port":8787,"proxy_port":12000,"web_external_access":true,"proxy_external_access":true}`
	legacyPath := filepath.Join(dataDir, "web-config.json")
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open("file:" + filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repos := store.NewRepos(db)
	admin, err := NewAdminConfigStore(&config.Config{DataDir: dataDir}, repos.App)
	if err != nil {
		t.Fatal(err)
	}
	got := admin.Config()
	if got.Username != "legacy-user" || got.Port != 39527 || got.ProxyPort != 12000 || !got.ProxyExternalAllowed() {
		t.Fatalf("legacy migration mismatch: %+v", got)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatal("web-config.json was not removed")
	}
	stored, err := repos.App.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stored.Admin.PasswordHash != hash || stored.Proxy.Port != 12000 {
		t.Fatal("legacy values were not stored in SQLite")
	}
}
