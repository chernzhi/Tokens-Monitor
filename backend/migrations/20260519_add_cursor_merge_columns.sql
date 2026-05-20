ALTER TABLE token_usage_logs ADD COLUMN IF NOT EXISTS source_kind VARCHAR(30);
ALTER TABLE token_usage_logs ADD COLUMN IF NOT EXISTS accuracy VARCHAR(20);
ALTER TABLE token_usage_logs ADD COLUMN IF NOT EXISTS correlation_key VARCHAR(200);
ALTER TABLE token_usage_logs ADD COLUMN IF NOT EXISTS merge_status VARCHAR(30);

CREATE INDEX IF NOT EXISTS idx_usage_cursor_correlation
    ON token_usage_logs(provider, correlation_key, source_kind, merge_status, request_at)
    WHERE provider = 'cursor' AND correlation_key IS NOT NULL;
