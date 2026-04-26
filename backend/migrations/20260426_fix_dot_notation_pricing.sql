-- ══════════════════════════════════════════════════════════════
-- 修复：点号格式 Claude 模型定价错误 + 补充缺失条目（2026-04-26）
--
-- 问题：
--   init.sql 中录入的点号格式 Claude 模型定价（来自 GitHub Copilot 上报的模型名）
--   价格与实际 Anthropic 官方定价不符：
--     claude-sonnet-4.6  录入 $1.5/$7.5 per 1M，实际应为 $3/$15 per 1M（少了一半）
--     claude-haiku-4.5   录入 $0.3/$1.5 per 1M，实际应为 $0.8/$4 per 1M（差距更大）
--
--   另外 claude-opus-4.7（点号，来自 Copilot）完全缺失，导致 cost_usd = 0。
--   gpt-5.5（Copilot 内部模型）也缺失定价条目。
--
-- 修复：
--   1. UPDATE claude-sonnet-4.6 到正确价格（与 claude-sonnet-4-6 一致）
--   2. UPDATE claude-haiku-4.5 到正确价格（与 claude-haiku-4-5 一致）
--   3. INSERT claude-opus-4.7（点号）
--   4. INSERT gpt-5.5（Copilot 内部 GPT-5 系列）
-- ══════════════════════════════════════════════════════════════

-- ── 1. 修正 claude-sonnet-4.6 定价 ────────────────────────────
-- 旧价：$1.5/$7.5 per 1M（$0.0015/$0.0075 per 1K）
-- 正确：$3/$15 per 1M（$0.003/$0.015 per 1K，与 claude-sonnet-4-6 / claude-sonnet-4.5 一致）
UPDATE model_pricing
SET input_price_per_1k  = 0.003,
    output_price_per_1k = 0.015
WHERE model_name = 'claude-sonnet-4.6'
  AND effective_from = '2026-01-01';

-- ── 2. 修正 claude-haiku-4.5 定价 ─────────────────────────────
-- 旧价：$0.3/$1.5 per 1M（$0.0003/$0.0015 per 1K）
-- 正确：$0.8/$4 per 1M（$0.0008/$0.004 per 1K，与 claude-haiku-4-5 / claude-3-5-haiku 一致）
UPDATE model_pricing
SET input_price_per_1k  = 0.0008,
    output_price_per_1k = 0.004
WHERE model_name = 'claude-haiku-4.5'
  AND effective_from = '2025-10-01';

-- ── 3. 补充 claude-opus-4.7（点号，Copilot 上报格式）──────────
-- 与 claude-opus-4-7（短横线 API 格式）同价：$15/$75 per 1M
INSERT INTO model_pricing (model_name, provider, input_price_per_1k, output_price_per_1k, effective_from)
VALUES ('claude-opus-4.7', 'anthropic', 0.015, 0.075, '2026-01-01')
ON CONFLICT (model_name, effective_from) DO UPDATE
    SET input_price_per_1k  = EXCLUDED.input_price_per_1k,
        output_price_per_1k = EXCLUDED.output_price_per_1k;

-- ── 4. 补充 gpt-5.5（Copilot 内部 GPT-5 系列） ────────────────
-- 无官方公开定价，参照 gpt-5.4 代理价（$2.5/$15 per 1M），
-- Copilot 场景下 cost_multiplier 0.1× 将在代码层应用。
INSERT INTO model_pricing (model_name, provider, input_price_per_1k, output_price_per_1k, effective_from)
VALUES ('gpt-5.5', 'openai', 0.0025, 0.015, '2026-01-01')
ON CONFLICT (model_name, effective_from) DO NOTHING;
