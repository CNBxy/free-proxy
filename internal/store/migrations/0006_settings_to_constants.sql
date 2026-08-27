-- +goose Up
-- The web console offered thirty-five settings. Almost all of them were tuning
-- values with one correct answer — probe concurrency, maintenance intervals,
-- handshake timeouts, the provider URLs — and every one of them was a number the
-- code had to keep treating as unknown. They are constants in internal/config
-- now, so the rows that stored them have nothing left to say.
--
-- What survives is what an operator actually decides: identity (username,
-- password, management path) and reachability (ports, external access).
--
-- Dropping the columns rather than leaving them unread is the point of the
-- change: a value that persists but is never loaded is worse than no value at
-- all, because the next person to read the database will believe it.
DROP TABLE discovery_settings;
DROP TABLE maintenance_settings;
DROP TABLE network_settings;

ALTER TABLE admin_settings DROP COLUMN session_ttl_seconds;

ALTER TABLE proxy_settings DROP COLUMN max_connections;
ALTER TABLE proxy_settings DROP COLUMN connect_timeout_seconds;
ALTER TABLE proxy_settings DROP COLUMN idle_timeout_seconds;
ALTER TABLE proxy_settings DROP COLUMN dns_server;

-- +goose Down
CREATE TABLE discovery_settings (
    id                    INTEGER PRIMARY KEY CHECK (id = 1),
    vpngate_api_url       TEXT NOT NULL DEFAULT 'https://www.vpngate.net/api/iphone/',
    discovery_limit       INTEGER NOT NULL DEFAULT 300,
    request_timeout_secs  REAL NOT NULL DEFAULT 15,
    ip_info_api_url       TEXT NOT NULL DEFAULT 'http://ip-api.com/batch?lang=zh-CN&fields=status,message,query,country,regionName,city,isp,org,as,asname,proxy,hosting,mobile',
    ip_info_cache_seconds INTEGER NOT NULL DEFAULT 604800
);
INSERT INTO discovery_settings (id) VALUES (1);

CREATE TABLE maintenance_settings (
    id                              INTEGER PRIMARY KEY CHECK (id = 1),
    enabled                         INTEGER NOT NULL DEFAULT 1,
    maintenance_interval_seconds    REAL NOT NULL DEFAULT 10800,
    health_check_interval_seconds   REAL NOT NULL DEFAULT 30,
    active_ping_interval_seconds    REAL NOT NULL DEFAULT 10,
    disconnected_retry_seconds      REAL NOT NULL DEFAULT 30,
    max_probe_concurrency           INTEGER NOT NULL DEFAULT 5,
    initial_connect_test_limit      INTEGER NOT NULL DEFAULT 10,
    manual_test_node_limit          INTEGER NOT NULL DEFAULT 5,
    openvpn_test_timeout_seconds    REAL NOT NULL DEFAULT 15,
    openvpn_connect_timeout_seconds REAL NOT NULL DEFAULT 35,
    invalid_backoff_seconds         INTEGER NOT NULL DEFAULT 1800,
    stale_node_grace_seconds        INTEGER NOT NULL DEFAULT 604800
);
INSERT INTO maintenance_settings (id) VALUES (1);

CREATE TABLE network_settings (
    id                             INTEGER PRIMARY KEY CHECK (id = 1),
    dns_repair_enabled             INTEGER NOT NULL DEFAULT 0,
    dns_repair_servers             TEXT NOT NULL DEFAULT '1.1.1.1,8.8.8.8',
    routing_setup_retries          INTEGER NOT NULL DEFAULT 3,
    routing_retry_interval_seconds REAL NOT NULL DEFAULT 1,
    routing_strict_rp_filter       INTEGER NOT NULL DEFAULT 0
);
INSERT INTO network_settings (id) VALUES (1);

ALTER TABLE admin_settings ADD COLUMN session_ttl_seconds INTEGER NOT NULL DEFAULT 2592000;

ALTER TABLE proxy_settings ADD COLUMN max_connections INTEGER NOT NULL DEFAULT 256;
ALTER TABLE proxy_settings ADD COLUMN connect_timeout_seconds REAL NOT NULL DEFAULT 20;
ALTER TABLE proxy_settings ADD COLUMN idle_timeout_seconds REAL NOT NULL DEFAULT 120;
ALTER TABLE proxy_settings ADD COLUMN dns_server TEXT NOT NULL DEFAULT '8.8.8.8';
