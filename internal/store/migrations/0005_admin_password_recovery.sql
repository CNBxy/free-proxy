-- +goose Up
-- The admin password used to exist only as a scrypt hash, so an operator who
-- forgot it had no way back in except rotating it — which restarts the service
-- and tears down the live tunnel. This box is single-tenant and root already
-- owns both the database file and the binary that reads it, so keeping a
-- recoverable copy next to the hash buys real operability at no practical
-- privilege boundary: `free-proxy credentials` can print it instead.
-- The hash stays authoritative for login verification.
ALTER TABLE admin_settings ADD COLUMN password_plain TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE admin_settings DROP COLUMN password_plain;
