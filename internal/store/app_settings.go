package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/masteralanlab/free-proxy/internal/config"
	"github.com/masteralanlab/free-proxy/internal/domain"
)

// AppSettingsRepository persists all settings that can be managed from the web
// control plane. Infrastructure/bootstrap values remain in free-proxy.env.
type settingsDB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type AppSettingsRepository struct {
	db   settingsDB
	root *sql.DB
}

func (r *AppSettingsRepository) Get(ctx context.Context) (domain.AppSettings, error) {
	var out domain.AppSettings
	var adminExternal, proxyEnabled, proxyExternal int64
	err := r.db.QueryRowContext(ctx, `SELECT username,password_hash,password_plain,secret_path,web_port,web_external_access FROM admin_settings WHERE id=1`).Scan(
		&out.Admin.Username, &out.Admin.PasswordHash, &out.Admin.Password, &out.Admin.SecretPath, &out.Admin.WebPort, &adminExternal)
	if err != nil {
		return out, err
	}
	err = r.db.QueryRowContext(ctx, `SELECT enabled,port,username,password_hash,external_access FROM proxy_settings WHERE id=1`).Scan(
		&proxyEnabled, &out.Proxy.Port, &out.Proxy.Username, &out.Proxy.PasswordHash, &proxyExternal)
	if err != nil {
		return out, err
	}
	out.Admin.WebExternalAccess = adminExternal != 0
	out.Admin.PasswordSet = out.Admin.PasswordHash != ""
	out.Proxy.Enabled = proxyEnabled != 0
	out.Proxy.ExternalAccess = proxyExternal != 0
	out.Proxy.PasswordSet = out.Proxy.PasswordHash != ""
	return out, nil
}

func (r *AppSettingsRepository) UpdateAdmin(ctx context.Context, s domain.AdminSettings) error {
	_, err := r.db.ExecContext(ctx, `UPDATE admin_settings SET username=?,password_hash=?,password_plain=?,secret_path=?,web_port=?,web_external_access=? WHERE id=1`,
		s.Username, s.PasswordHash, s.Password, s.SecretPath, s.WebPort, b2i(s.WebExternalAccess))
	return err
}

func (r *AppSettingsRepository) UpdateProxy(ctx context.Context, s domain.ProxyServiceSettings) error {
	_, err := r.db.ExecContext(ctx, `UPDATE proxy_settings SET enabled=?,port=?,username=?,password_hash=?,external_access=? WHERE id=1`,
		b2i(s.Enabled), s.Port, s.Username, s.PasswordHash, b2i(s.ExternalAccess))
	return err
}

func (r *AppSettingsRepository) UpdateAll(ctx context.Context, s domain.AppSettings) error {
	tx, err := r.root.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := &AppSettingsRepository{db: tx}
	if err = q.UpdateAdmin(ctx, s.Admin); err != nil {
		return err
	}
	if err = q.UpdateProxy(ctx, s.Proxy); err != nil {
		return err
	}
	return tx.Commit()
}

// InitializeFromLegacyEnv imports the former database-owned environment values
// once. Subsequent starts always use the database and ignore those legacy keys.
func (r *AppSettingsRepository) InitializeFromLegacyEnv(ctx context.Context, cfg *config.Config, proxyPasswordHash string) error {
	var marker string
	err := r.db.QueryRowContext(ctx, `SELECT value FROM app_metadata WHERE key='legacy_env_imported'`).Scan(&marker)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	tx, err := r.root.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	webPort := cfg.WebPort
	if webPort == 0 || webPort == 8787 {
		webPort = 39527
	}
	queries := []struct {
		q    string
		args []any
	}{
		{`UPDATE admin_settings SET web_port=? WHERE id=1`, []any{webPort}},
		{`UPDATE proxy_settings SET enabled=?,port=?,username=?,password_hash=? WHERE id=1`, []any{b2i(cfg.ProxyEnabled), cfg.ProxyPort, cfg.ProxyUsername, proxyPasswordHash}},
	}
	for _, item := range queries {
		if _, err = tx.ExecContext(ctx, item.q, item.args...); err != nil {
			return fmt.Errorf("import legacy settings: %w", err)
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO app_metadata(key,value) VALUES('legacy_env_imported','1')`); err != nil {
		return err
	}
	return tx.Commit()
}

// ApplyToConfig hydrates existing services from the database-backed settings.
// This keeps one consistent runtime snapshot while settings changes trigger a
// restart. Only the listener and identity values travel this way now; the rest
// of what a service needs is a constant it reads from internal/config directly.
func ApplyToConfig(cfg *config.Config, s domain.AppSettings) {
	cfg.WebHost, cfg.ProxyHost = "0.0.0.0", "0.0.0.0"
	cfg.WebPort = s.Admin.WebPort
	cfg.ProxyEnabled, cfg.ProxyPort = s.Proxy.Enabled, s.Proxy.Port
	cfg.ProxyUsername, cfg.ProxyPassword = s.Proxy.Username, ""
}
