-- Optional additive DDL if 043 was applied before response_status existed.
-- Startup AutoMigrate also adds this column for tests/dev.
SET @wh_idem_status_ddl = IF(
 (SELECT COUNT(*) FROM information_schema.columns
  WHERE table_schema=DATABASE() AND table_name='project_webhook_idempotency' AND column_name='response_status')=0,
 'ALTER TABLE project_webhook_idempotency ADD COLUMN response_status INT NOT NULL DEFAULT 200',
 'SELECT 1');
PREPARE wh_idem_status_stmt FROM @wh_idem_status_ddl;
EXECUTE wh_idem_status_stmt;
DEALLOCATE PREPARE wh_idem_status_stmt;
