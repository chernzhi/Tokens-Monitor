"""
重算所有 source=client-mitm-estimate 且 cost_usd=0 的历史记录成本。
在后端容器内运行：cd /app && python recalc_estimate_costs.py
"""

import asyncio
import logging
from collections import defaultdict
from datetime import date

logging.basicConfig(level=logging.INFO, format="%(message)s")
log = logging.getLogger(__name__)

# ── 引入后端模块（需要在 /app 目录下运行）──
from app.config import get_settings
from app.database import AsyncSessionLocal
from app.pricing import calc_cost_usd
from app.canonical import canonical_provider_key
from sqlalchemy import text

ESTIMATE_SOURCE = "client-mitm-estimate"
settings = get_settings()
USD_TO_CNY = settings.USD_TO_CNY


async def load_pricing(session) -> dict[str, tuple[float, float]]:
    rows = await session.execute(
        text("SELECT model_name, input_price_per_1k, output_price_per_1k FROM model_pricing")
    )
    return {r[0]: (float(r[1]), float(r[2])) for r in rows}


async def run():
    async with AsyncSessionLocal() as session:
        pricing = await load_pricing(session)
        log.info(f"Loaded {len(pricing)} pricing entries")

        # 查所有 estimate 记录中 cost=0 且有 token 的
        rows = await session.execute(text("""
            SELECT id, model_name, vendor, input_tokens, output_tokens, total_tokens,
                   cost_multiplier, DATE(request_at) AS day
            FROM token_usage_logs
            WHERE source = :src
              AND COALESCE(cost_usd, 0) = 0
              AND COALESCE(total_tokens, 0) > 0
            ORDER BY id
        """), {"src": ESTIMATE_SOURCE})
        records = rows.fetchall()
        log.info(f"Found {len(records)} estimate records with cost=0 to recalculate")

        updated = 0
        skipped = 0
        affected_days: set[date] = set()
        model_stats: dict[str, list] = defaultdict(lambda: [0, 0.0])  # [count, cost_usd]

        for r in records:
            rid, model_name, vendor, inp, out, total, mult, day = r
            provider_key = canonical_provider_key(vendor)

            cost_usd = calc_cost_usd(
                pricing,
                model_name,
                int(inp or 0),
                int(out or 0),
                int(total or 0),
                provider=provider_key,
                cost_multiplier=float(mult) if mult else None,
            )

            if cost_usd <= 0:
                skipped += 1
                continue

            cost_cny = round(cost_usd * USD_TO_CNY, 4)
            await session.execute(text("""
                UPDATE token_usage_logs
                SET cost_usd = :usd, cost_cny = :cny
                WHERE id = :id
            """), {"usd": cost_usd, "cny": cost_cny, "id": rid})

            updated += 1
            affected_days.add(day)
            model_stats[model_name][0] += 1
            model_stats[model_name][1] += cost_usd

        await session.commit()
        log.info(f"Updated {updated} records, skipped {skipped} (no pricing match)")

        # 按成本降序打印模型统计
        sorted_models = sorted(model_stats.items(), key=lambda x: -x[1][1])
        log.info("\n== 各模型更新统计 ==")
        for m, (cnt, cost) in sorted_models[:20]:
            log.info(f"  {m}: {cnt} records, ${cost:.4f} (¥{cost*USD_TO_CNY:.2f})")

        # 重建 daily_usage_summary
        if affected_days:
            log.info(f"\n重建 {len(affected_days)} 天的 daily_usage_summary...")
            from app.services.aggregator import aggregate_daily
            for day in sorted(affected_days):
                await aggregate_daily(day)
                log.info(f"  ✓ {day}")

        log.info("\n✅ Done!")


if __name__ == "__main__":
    asyncio.run(run())
