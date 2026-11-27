-- Project inbound webhook channels: per-project URL + secret, event log,
-- nonce / idempotency tables with TTL cleanup by operators or periodic jobs.
-- Manual apply; Open() only AutoMigrates missing DDL for tests/dev.

CREATE TABLE IF NOT EXISTS project_webhook_channels (
  id VARCHAR(64) NOT NULL PRIMARY KEY,
  project_id VARCHAR(128) NOT NULL,
  delivery_token_hash VARCHAR(64) NOT NULL,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  payload JSON,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  UNIQUE KEY idx_project_webhook_channels_token (delivery_token_hash),
  KEY idx_project_webhook_channels_project (project_id)
) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS project_webhook_secrets (
  id VARCHAR(64) NOT NULL PRIMARY KEY,
  channel_id VARCHAR(64) NOT NULL,
  version INT NOT NULL,
  secret_hash VARCHAR(64) NOT NULL,
  secret_enc TEXT NOT NULL,
  secret_prefix VARCHAR(32) NOT NULL,
  created_at DATETIME(3) NOT NULL,
  grace_until DATETIME(3) NULL,
  UNIQUE KEY idx_project_webhook_secrets_channel_version (channel_id, version),
  KEY idx_project_webhook_secrets_hash (secret_hash),
  KEY idx_project_webhook_secrets_channel (channel_id)
) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS project_webhook_events (
  id VARCHAR(64) NOT NULL PRIMARY KEY,
  channel_id VARCHAR(64) NOT NULL,
  project_id VARCHAR(128) NOT NULL,
  status VARCHAR(32) NOT NULL,
  reason VARCHAR(512) NULL,
  issue_id VARCHAR(128) NULL,
  issue_number INT NULL,
  task_id VARCHAR(128) NULL,
  run_id VARCHAR(128) NULL,
  idempotency_key_id VARCHAR(64) NULL,
  client_ip VARCHAR(64) NULL,
  payload_bytes INT NULL,
  payload_summary MEDIUMTEXT NULL,
  received_at DATETIME(3) NOT NULL,
  KEY idx_project_webhook_events_channel_time (channel_id, received_at),
  KEY idx_project_webhook_events_project_time (project_id, received_at),
  KEY idx_project_webhook_events_issue (issue_id)
) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS project_webhook_nonces (
  channel_id VARCHAR(64) NOT NULL,
  nonce_hash VARCHAR(64) NOT NULL,
  expires_at DATETIME(3) NOT NULL,
  PRIMARY KEY (channel_id, nonce_hash),
  KEY idx_project_webhook_nonces_expires (expires_at)
) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS project_webhook_idempotency (
  channel_id VARCHAR(64) NOT NULL,
  key_hash VARCHAR(64) NOT NULL,
  issue_id VARCHAR(128) NOT NULL,
  issue_number INT NOT NULL,
  task_id VARCHAR(128) NULL,
  run_id VARCHAR(128) NULL,
  response_json MEDIUMTEXT NULL,
  response_status INT NOT NULL DEFAULT 200,
  expires_at DATETIME(3) NOT NULL,
  PRIMARY KEY (channel_id, key_hash),
  KEY idx_project_webhook_idempotency_expires (expires_at)
) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
