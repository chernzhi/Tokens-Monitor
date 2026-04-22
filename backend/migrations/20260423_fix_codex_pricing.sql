-- ══════════════════════════════════════════════════════════════
-- 修复：更正 Codex/Copilot 模型定价（2026-04-23 第二批）
--
-- 1. codex-5.3 (provider=openai) = gpt-5.3-codex，OpenAI 直接 API 调用，
--    有真实公开定价：$1.75/$14 per 1M tokens
-- 2. gpt-5.3-codex 之前代理价偏低（用了 gpt-4.1 代理价），更正为官方价
-- 3. gpt-5.2-codex 同步更正（与 gpt-5.3-codex 同级别 Codex 模型）
-- 4. grok-code-fast-1 通过 Copilot 接入，参照 xAI 公开价格
-- 5. capi-noe-ptuc-h200-ib-gpt-5-mini-2025-08-07 = GPT-5-mini on H200，
--    参照 gpt-5-mini 已有定价
-- ══════════════════════════════════════════════════════════════

-- ── codex-5.3：OpenAI 直接 API，公开定价 ─────────────────────
-- $1.75/1M input = $0.00175/1K；$14/1M output = $0.014/1K
INSERT INTO model_pricing (model_name, provider, input_price_per_1k, output_price_per_1k, effective_from)
VALUES ('codex-5.3', 'openai', 0.00175, 0.014, '2026-02-24')
ON CONFLICT (model_name, effective_from) DO UPDATE
    SET input_price_per_1k  = EXCLUDED.input_price_per_1k,
        output_price_per_1k = EXCLUDED.output_price_per_1k;

-- ── gpt-5.3-codex：更正为官方 Codex 价格（之前用了 gpt-4.1 代理价）─
-- Copilot 场景下 cost_multiplier=0.1 会自动应用
UPDATE model_pricing
SET input_price_per_1k  = 0.00175,
    output_price_per_1k = 0.014
WHERE model_name = 'gpt-5.3-codex'
  AND effective_from = '2026-01-01';

-- ── gpt-5.2-codex：同步更正（gpt-5.2 与 5.3 同代 Codex 系列）─
UPDATE model_pricing
SET input_price_per_1k  = 0.00175,
    output_price_per_1k = 0.014
WHERE model_name = 'gpt-5.2-codex'
  AND effective_from = '2026-01-01';

-- ── grok-code-fast-1：Copilot 官方模型，参照 xAI 公开价格 ────
-- xAI grok fast 系列：$0.20/$0.50 per 1M；Copilot 订阅内含，成本仅作内部参考
INSERT INTO model_pricing (model_name, provider, input_price_per_1k, output_price_per_1k, effective_from)
VALUES ('grok-code-fast-1', 'xai', 0.0002, 0.0005, '2026-01-01')
ON CONFLICT (model_name, effective_from) DO NOTHING;

-- ── capi-noe-ptuc-h200-ib-gpt-5-mini-2025-08-07：GPT-5-mini on H200 ─
-- 底层模型为 gpt-5-mini，参照 gpt-5-mini 已有定价 ($0.0005/$0.003 per 1K)
-- Copilot 订阅内含，成本仅作内部参考
INSERT INTO model_pricing (model_name, provider, input_price_per_1k, output_price_per_1k, effective_from)
VALUES ('capi-noe-ptuc-h200-ib-gpt-5-mini-2025-08-07', 'openai', 0.0005, 0.003, '2025-08-07')
ON CONFLICT (model_name, effective_from) DO NOTHING;
