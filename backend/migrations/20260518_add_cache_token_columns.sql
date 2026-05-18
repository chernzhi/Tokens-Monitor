-- 新增缓存命中观测列：cache_read_tokens / cache_creation_tokens
-- 仅用于展示缓存效果，不参与 total_tokens / cost_* 计算（input_tokens 已含等效折算）。
-- 旧记录默认为 0。

ALTER TABLE token_usage_logs
    ADD COLUMN IF NOT EXISTS cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_creation_tokens BIGINT NOT NULL DEFAULT 0;
