package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// Migration 0006 drops four columns from proxy_settings and one from
// admin_settings. SQLite implements DROP COLUMN by rewriting the table, so this
// is the one migration in the set that can lose the credentials it is not
// touching. Upgrade a database that already holds settings and check they came
// through.
func TestSettingsSurviveTheDropColumnMigration(t *testing.T) {
	db, err := Open("file:" + filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	migrateTo(t, db, 5)
	exec(t, db, `UPDATE admin_settings SET username='admin',password_hash='admin-hash',password_plain='Secret1Pass',secret_path='abc123',web_port=39527,web_external_access=1,session_ttl_seconds=999 WHERE id=1`)
	exec(t, db, `UPDATE proxy_settings SET enabled=1,port=9527,username='proxy-user',password_hash='proxy-hash',external_access=1,max_connections=77,dns_server='1.1.1.1' WHERE id=1`)

	if err := Migrate(db); err != nil {
		t.Fatalf("upgrade: %v", err)
	}

	got, err := NewRepos(db).App.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Admin.Username != "admin" || got.Admin.PasswordHash != "admin-hash" ||
		got.Admin.Password != "Secret1Pass" || got.Admin.SecretPath != "abc123" ||
		got.Admin.WebPort != 39527 || !got.Admin.WebExternalAccess {
		t.Errorf("admin settings did not survive the upgrade: %+v", got.Admin)
	}
	if !got.Proxy.Enabled || got.Proxy.Port != 9527 || got.Proxy.Username != "proxy-user" ||
		got.Proxy.PasswordHash != "proxy-hash" || !got.Proxy.ExternalAccess {
		t.Errorf("proxy settings did not survive the upgrade: %+v", got.Proxy)
	}
}

func migrateTo(t *testing.T, db *sql.DB, version int64) {
	t.Helper()
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(db, "migrations", version); err != nil {
		t.Fatalf("migrate to %d: %v", version, err)
	}
}

func exec(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}
