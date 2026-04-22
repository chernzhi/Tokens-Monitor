"""
修复：重算 cost_usd = 0 但实际有 token 消耗的历史记录。

适用场景：新增了模型定价条目（如 claude-sonnet-4-6、claude-haiku-4-5-20251001 等）后，
补充计算之前因定价未命中而漏算的成本。

使用方法：
    cd /opt/token-monitor/backend
    python scripts/recalc_zero_cost.py

只处理：
    - cost_usd = 0 且 total_tokens > 0
    - source 不含 'estimate'（estimate 流量不计费）
    - 重算结果 > 0（定价确实命中了）

不处理：
    - source LIKE '%estimate%'（正确行为：不计费）
    - vscode-agentic-search-router-*、capi-noe-* 等无定价的内部路由模型（维持 $0）
"""

from __future__ import annotations

import asyncio
import os
import sys
from datetime import date
from pathlib import Path

from sqlalchemy import Date, cast, func, select

if "__file__" in globals() and not str(__file__).startswith("<"):
    ROOT = Path(__file__).resolve().parent.parent
else:
    ROOT = Path.cwd()
os.chdir(ROOT)
sys.path.insert(0, str(ROOT))

from app.config import settings
from app.database import async_session
from app.models import ModelPricing, TokenUsageLog
from app.pricing import calc_cost_usd
from app.services.aggregator import aggregate_daily


async def _load_pricing(db) -> dict[str, tuple[float, float]]:
    result = await db.execute(select(ModelPricing).where(ModelPricing.effective_to.is_(None)))
    return {
        row.model_name: (float(row.input_price_per_1k), float(row.output_price_per_1k))
        for row in result.scalars().all()
    }


async def main() -> None:
    local_day = cast(func.timezone(settings.DASHBOARD_TIMEZONE, TokenUsageLog.request_at), Date)

    async with async_session() as db:
        pricing = await _load_pricing(db)
        print(f"已加载 {len(pricing)} 条定价")

        # 查出所有 cost=0 但有 tokens 的非估算记录
        result = await db.execute(
            select(
                TokenUsageLog.id,
                TokenUsageLog.model_name,
                TokenUsageLog.provider,
                TokenUsageLog.input_tokens,
                TokenUsageLog.output_tokens,
                TokenUsageLog.total_tokens,
                local_day.label("local_day"),
            ).where(
                TokenUsageLog.total_tokens > 0,
                (TokenUsageLog.cost_usd == 0) | TokenUsageLog.cost_usd.is_(None),
                ~TokenUsageLog.source.contains("estimate"),
            )
        )

        rows = result.all()
        print(f"待检查记录数：{len(rows)}")

        affected_dates: set[date] = set()
        updated = 0
        skipped_no_pricing = 0
        model_stats: dict[str, int] = {}

        for row in rows:
            new_usd = calc_cost_usd(
                pricing,
                row.model_name,
                int(row.input_tokens or 0),
                int(row.output_tokens or 0),
                int(row.total_tokens or 0),
                provider=row.provider,
            )
            if new_usd <= 0:
                skipped_no_pricing += 1
                continue

            new_cny = round(new_usd * settings.USD_TO_CNY, 4)
            log_row = await db.get(TokenUsageLog, row.id)
            if log_row is None:
                continue

            log_row.cost_usd = new_usd
            log_row.cost_cny = new_cny
            updated += 1
            model_stats[row.model_name] = model_stats.get(row.model_name, 0) + 1
            if isinstance(row.local_day, date):
                affected_dates.add(row.local_day)

        await db.commit()

    print(f"\n=== 重算结果 ===")
    print(f"  更新记录数：{updated}")
    print(f"  无定价跳过：{skipped_no_pricing}")
    print(f"  受影响日期：{len(affected_dates)}")
    print(f"\n  按模型统计：")
    for model, count in sorted(model_stats.items(), key=lambda x: -x[1]):
        print(f"    {model}: {count} 条")

    rebuilt = 0
    for day in sorted(affected_dates):
        await aggregate_daily(day)
        rebuilt += 1
        print(f"  重建汇总：{day}")

    print(f"\n  重建 daily_summary：{rebuilt} 天")
    print("✅ 完成")


if __name__ == "__main__":
    asyncio.run(main())
