-- ══════════════════════════════════════════════════════════════
-- 修复：补充缺失的模型定价条目（2026-04-23）
--
-- 背景：
--   Anthropic API 下发的模型 ID 使用短横线（claude-sonnet-4-6），
--   但之前定价表录入时使用了点号（claude-sonnet-4.6），导致前缀匹配失败，
--   相关记录 cost_usd = 0。
--
-- 处理方式：
--   1. 为 Claude 短横线格式 ID 添加定价别名，与对应点号版本价格一致
--   2. 为 GitHub Copilot 内部代理模型添加近似定价（标注为代理价）
-- ══════════════════════════════════════════════════════════════

-- ── Anthropic Claude —— API 格式（短横线）别名 ────────────────
-- Sonnet 4.6  API ID: claude-sonnet-4-6  (官方 $/1M: $3 in / $15 out)
INSERT INTO model_pricing (model_name, provider, input_price_per_1k, output_price_per_1k, effective_from)
VALUES ('claude-sonnet-4-6', 'anthropic', 0.003, 0.015, '2026-01-01')
ON CONFLICT (model_name, effective_from) DO NOTHING;

-- Haiku 4.5   API ID: claude-haiku-4-5  (官方 $/1M: $0.80 in / $4 out)
INSERT INTO model_pricing (model_name, provider, input_price_per_1k, output_price_per_1k, effective_from)
VALUES ('claude-haiku-4-5', 'anthropic', 0.0008, 0.004, '2025-10-01')
ON CONFLICT (model_name, effective_from) DO NOTHING;

-- Haiku 4.5 带日期后缀版本  API ID: claude-haiku-4-5-20251001
INSERT INTO model_pricing (model_name, provider, input_price_per_1k, output_price_per_1k, effective_from)
VALUES ('claude-haiku-4-5-20251001', 'anthropic', 0.0008, 0.004, '2025-10-01')
ON CONFLICT (model_name, effective_from) DO NOTHING;

-- Opus 4.6    API ID: claude-opus-4-6  (短横线别名，与 claude-opus-4.6 同价)
INSERT INTO model_pricing (model_name, provider, input_price_per_1k, output_price_per_1k, effective_from)
VALUES ('claude-opus-4-6', 'anthropic', 0.005, 0.025, '2026-01-01')
ON CONFLICT (model_name, effective_from) DO NOTHING;

-- Opus 4.7    API ID: claude-opus-4-7  (新模型，暂用 Opus 标准定价)
INSERT INTO model_pricing (model_name, provider, input_price_per_1k, output_price_per_1k, effective_from)
VALUES ('claude-opus-4-7', 'anthropic', 0.015, 0.075, '2026-01-01')
ON CONFLICT (model_name, effective_from) DO NOTHING;

-- ── GitHub Copilot 内部代理模型（代理价，实际折扣由 cost_multiplier 0.1x 应用）─
-- gpt-5.2-codex: Copilot Codex 完成模型，代理价参照 gpt-4.1
INSERT INTO model_pricing (model_name, provider, input_price_per_1k, output_price_per_1k, effective_from)
VALUES ('gpt-5.2-codex', 'openai', 0.002, 0.008, '2026-01-01')
ON CONFLICT (model_name, effective_from) DO NOTHING;

-- gpt-5.3-codex: 同上，GITHUB_COPILOT_COST_MULTIPLIERS 已配置 0.1x 折扣
INSERT INTO model_pricing (model_name, provider, input_price_per_1k, output_price_per_1k, effective_from)
VALUES ('gpt-5.3-codex', 'openai', 0.002, 0.008, '2026-01-01')
ON CONFLICT (model_name, effective_from) DO NOTHING;

-- gpt-5-mini: Copilot 内部模型，代理价参照 gpt-5.4-mini
INSERT INTO model_pricing (model_name, provider, input_price_per_1k, output_price_per_1k, effective_from)
VALUES ('gpt-5-mini', 'openai', 0.0005, 0.003, '2026-01-01')
ON CONFLICT (model_name, effective_from) DO NOTHING;
