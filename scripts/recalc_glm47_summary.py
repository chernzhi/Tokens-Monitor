"""
修复：z-ai/glm-4.7 存量记录成本修正后重建 daily_usage_summary

背景：
  - api.ofox.ai 以 z-ai/glm-4.7 为模型名上报，共 335 条历史记录 cost_usd = 0
  - 20260426_add_z_ai_glm47_pricing.sql 已完成定价插入 + token_usage_logs 更新
  - 本脚本仅负责重建受影响日期的 daily_usage_summary

使用方式：
  python scripts/recalc_glm47_summary.py [--dry-run]
"""

from __future__ import annotations

import argparse
import asyncio
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT / "backend"))

from app.database import async_session
from app.services.aggregator import aggregate_daily

from sqlalchemy import text


async def recalc(dry_run: bool) -> None:
    async with async_session() as session:
        rows = (
            await session.execute(
                text(
                    """
                    SELECT DISTINCT request_at::date AS day
                    FROM token_usage_logs
                    WHERE model_name = 'z-ai/glm-4.7'
                    ORDER BY day
                    """
                )
            )
        ).fetchall()

    affected_dates = [r.day for r in rows]
    print(f"受影响日期：{len(affected_dates)} 天")
    for d in affected_dates:
        print(f"  {d}")

    if dry_run:
        print("[DRY-RUN] 不执行重建")
        return

    print("\n重建 daily_usage_summary …")
    for d in affected_dates:
        await aggregate_daily(d)
        print(f"  rebuilt {d}")
    print("完成。")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()
    asyncio.run(recalc(args.dry_run))


if __name__ == "__main__":
    main()
