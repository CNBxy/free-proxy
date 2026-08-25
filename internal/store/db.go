// Package store is the data-access layer: a thin repository facade over the
// sqlc-generated queries plus goose-managed, embedded schema migrations.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"strings"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open opens the SQLite database (modernc pure-Go driver, no CGO). The DSN is
// expected to carry the WAL/busy_timeout pragmas set in config.
func Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	restrictPermissions(dsn)
	return db, nil
}

// restrictPermissions keeps the database owner-only. It holds the admin and
// proxy credentials — including a recoverable copy of the admin password — and
// the data directory itself is world-readable, so the default 0644 the driver
// creates would expose them to every local account. Best-effort: a database on
// a filesystem that cannot represent the mode is not a reason to refuse to run.
func restrictPermissions(dsn string) {
	path := dbFilePath(dsn)
	if path == "" {
		return
	}
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if _, err := os.Stat(p); err == nil {
			_ = os.Chmod(p, 0o600)
		}
	}
}

// dbFilePath returns the on-disk file a sqlite DSN points at, or "" for
// in-memory and other non-file databases.
func dbFilePath(dsn string) string {
	path := strings.TrimPrefix(dsn, "file:")
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	if path == "" || strings.HasPrefix(path, ":") {
		return ""
	}
	return path
}

// Migrate applies all embedded goose migrations up to head.
func Migrate(db *sql.DB) error {
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}
	return goose.Up(db, "migrations")
}

// SchemaTables returns the user table names, used by the `status` command.
func SchemaTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}
