-- +goose Up
ALTER TABLE runtime_settings ADD COLUMN country_filters TEXT NOT NULL DEFAULT '[]';
ALTER TABLE runtime_settings ADD COLUMN priority_order TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE runtime_settings DROP COLUMN country_filters;
ALTER TABLE runtime_settings DROP COLUMN priority_order;
