-- ══════════════════════════════════════════════════════════════
-- 补充：z-ai/glm-4.7 定价 + 存量记录成本修正（2026-04-26）
--
-- 背景：
--   api.ofox.ai 以 z-ai/glm-4.7 为模型名上报，该模型为智谱 GLM-4 系列新版本。
--   官方定价参照 glm-4-plus（$7/$7 per 1M），init.sql 中无此条目，
--   导致 335 条历史记录 cost_usd = 0，累计 ~18.9M tokens 未计费。
--
-- 修复：
--   1. 插入 z-ai/glm-4.7 定价（$0.007/$0.007 per 1K）
--   2. 更新存量 cost_usd=0 记录，重算成本
-- ══════════════════════════════════════════════════════════════

-- ── 1. 插入 z-ai/glm-4.7 定价 ─────────────────────────────────
-- 与 glm-4-plus 同价：$7/$7 per 1M（$0.007/$0.007 per 1K）
INSERT INTO model_pricing (model_name, provider, input_price_per_1k, output_price_per_1k, effective_from)
VALUES ('z-ai/glm-4.7', 'zhipu', 0.007, 0.007, '2026-01-01')
ON CONFLICT (model_name, effective_from) DO UPDATE
    SET input_price_per_1k  = EXCLUDED.input_price_per_1k,
        output_price_per_1k = EXCLUDED.output_price_per_1k;

-- ── 2. 修正存量 cost_usd=0 记录 ───────────────────────────────
-- 注：input_price = output_price，70/30 split 与直接用 total_tokens 等价
UPDATE token_usage_logs
SET
    cost_usd = ROUND(
        CASE
            WHEN COALESCE(input_tokens, 0) = 0 AND COALESCE(output_tokens, 0) = 0 THEN
                -- 无拆分时直接用 total_tokens（等价于 70/30 因价格相等）
                COALESCE(total_tokens, 0) / 1000.0 * 0.007
            ELSE
                COALESCE(input_tokens, 0)  / 1000.0 * 0.007 +
                COALESCE(output_tokens, 0) / 1000.0 * 0.007
        END,
        6
    ),
    cost_cny = ROUND(
        CASE
            WHEN COALESCE(input_tokens, 0) = 0 AND COALESCE(output_tokens, 0) = 0 THEN
                COALESCE(total_tokens, 0) / 1000.0 * 0.007 * 7.25
            ELSE
                (COALESCE(input_tokens, 0)  / 1000.0 * 0.007 +
                 COALESCE(output_tokens, 0) / 1000.0 * 0.007) * 7.25
        END,
        4
    )
WHERE model_name = 'z-ai/glm-4.7'
  AND COALESCE(cost_usd, 0) = 0
  AND COALESCE(total_tokens, 0) > 0;
