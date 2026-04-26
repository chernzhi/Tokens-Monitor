-- 补充 opaque 估算流量中缺失定价的模型
-- glm-5: GLM-5 通过 aws-q 兼容端点，按 glm-4-plus 同级定价（¥0.05/1K tokens）
-- yi-S:  Yi Spark (S=Spark) 通过 chatgpt 兼容端点，按 yi-spark 同级定价

INSERT INTO model_pricing (model_name, provider, input_price_per_1k, output_price_per_1k, effective_from)
VALUES
    ('glm-5',  'zhipu',  0.007000, 0.007000, '2026-01-01'),
    ('yi-S',   'yi',     0.000140, 0.000140, '2026-01-01')
ON CONFLICT (model_name, effective_from) DO UPDATE
    SET input_price_per_1k  = EXCLUDED.input_price_per_1k,
        output_price_per_1k = EXCLUDED.output_price_per_1k;

-- 补算历史记录成本
UPDATE token_usage_logs
SET
    cost_usd = ROUND(
        CASE
            WHEN COALESCE(input_tokens, 0) = 0 AND COALESCE(output_tokens, 0) = 0 THEN
                COALESCE(total_tokens, 0) * 0.7 / 1000.0 * p.input_price_per_1k +
                COALESCE(total_tokens, 0) * 0.3 / 1000.0 * p.output_price_per_1k
            ELSE
                COALESCE(input_tokens, 0) / 1000.0 * p.input_price_per_1k +
                COALESCE(output_tokens, 0) / 1000.0 * p.output_price_per_1k
        END, 6),
    cost_cny = ROUND(
        CASE
            WHEN COALESCE(input_tokens, 0) = 0 AND COALESCE(output_tokens, 0) = 0 THEN
                (COALESCE(total_tokens, 0) * 0.7 / 1000.0 * p.input_price_per_1k +
                 COALESCE(total_tokens, 0) * 0.3 / 1000.0 * p.output_price_per_1k) * 7.25
            ELSE
                (COALESCE(input_tokens, 0) / 1000.0 * p.input_price_per_1k +
                 COALESCE(output_tokens, 0) / 1000.0 * p.output_price_per_1k) * 7.25
        END, 4)
FROM (
    VALUES
        ('glm-5·opaque(估算)',  0.007000::numeric, 0.007000::numeric),
        ('yi-S·opaque(估算)',   0.000140::numeric, 0.000140::numeric)
) AS p(model_name, input_price_per_1k, output_price_per_1k)
WHERE token_usage_logs.model_name = p.model_name
  AND COALESCE(token_usage_logs.cost_usd, 0) = 0
  AND COALESCE(token_usage_logs.total_tokens, 0) > 0;
