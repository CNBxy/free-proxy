package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/masteralanlab/free-proxy/internal/config"
)

// The dashboard saves the admin password through the whole-settings write, the
// one path no security-package test exercises. UpdateAdmin is covered there.
func TestAdminPasswordSurvivesWholeSettingsWrite(t *testing.T) {
	db, err := Open("file:" + filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	repo := NewRepos(db).App
	ctx := context.Background()
	all, err := repo.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	all.Admin.Username, all.Admin.PasswordHash, all.Admin.Password = "admin", "hash", "Secret1Pass"
	if err := repo.UpdateAll(ctx, all); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Admin.Password != "Secret1Pass" {
		t.Fatalf("password after UpdateAll = %q", got.Admin.Password)
	}
}

func TestAppSettingsLegacyImportRunsOnce(t *testing.T) {
	db, err := Open("file:" + filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	repo := NewRepos(db).App
	cfg := &config.Config{ProxyEnabled: true, ProxyPort: 12345, ProxyUsername: "proxy-user"}
	ctx := context.Background()
	if err := repo.InitializeFromLegacyEnv(ctx, cfg, "proxy-hash"); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Admin.WebPort != 39527 || got.Proxy.Port != 12345 || got.Proxy.Username != "proxy-user" || got.Proxy.PasswordHash != "proxy-hash" {
		t.Fatalf("legacy import mismatch: %+v", got)
	}
	cfg.ProxyPort = 54321
	if err := repo.InitializeFromLegacyEnv(ctx, cfg, "changed"); err != nil {
		t.Fatal(err)
	}
	again, err := repo.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if again.Proxy.Port != 12345 || again.Proxy.PasswordHash != "proxy-hash" {
		t.Fatal("legacy settings were imported more than once")
	}
}
