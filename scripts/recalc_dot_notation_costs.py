"""
修复：重算点号格式 Claude 模型历史记录成本

背景：
  - init.sql 中 claude-sonnet-4.6、claude-haiku-4.5 定价偏低（分别为正确价的 50%、37.5%）
  - claude-opus-4.7（点号格式，来自 Copilot）完全缺失定价，cost_usd = 0
  - 本次迁移（20260426_fix_dot_notation_pricing.sql）已修正定价表

本脚本：
  1. 加载修正后的定价
  2. 找出受影响模型的历史记录（cost_usd 需要修正的）
  3. 用新定价重算 cost_usd / cost_cny
  4. 批量更新
  5. 重建受影响日期的 daily_usage_summary

使用方式（在 backend/ 目录或项目根目录下运行）：
  python scripts/recalc_dot_notation_costs.py [--dry-run]

Options:
  --dry-run   仅输出受影响记录数与预期改动，不实际写库
"""

from __future__ import annotations

import argparse
import asyncio
import sys
from datetime import date, timedelta
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT / "backend"))

from app.config import settings
from app.database import async_session
from app.pricing import _normalize_model_name, calc_cost_usd
from app.services.aggregator import aggregate_daily

from sqlalchemy import select, text, update

# 受影响的模型：这些模型的定价在本次迁移中被修正
# key = model_name（数据库中实际存储的值），value = 简要说明
AFFECTED_MODELS: dict[str, str] = {
    "claude-sonnet-4.6": "价格修正：$1.5→$3 per 1M input",
    "claude-haiku-4.5": "价格修正：$0.3→$0.8 per 1M input",
    "claude-opus-4.7": "新增定价：此前 cost=0",
    # 点号被规范化后统一查到短横线定价条目，同样覆盖下面这些（如果有的话）
    "claude-opus-4.6": "价格确认（与 claude-opus-4-6 一致，$5/$25 per 1M）",
}


async def load_pricing() -> dict[str, tuple[float, float]]:
    """从数据库加载最新定价表。"""
    async with async_session() as session:
        rows = await session.execute(
            text(
                """
                SELECT DISTINCT ON (model_name) model_name, input_price_per_1k, output_price_per_1k
                FROM model_pricing
                ORDER BY model_name, effective_from DESC
                """
            )
        )
        return {r.model_name: (float(r.input_price_per_1k), float(r.output_price_per_1k)) for r in rows}


async def recalc(dry_run: bool) -> None:
    usd_to_cny: float = float(getattr(settings, "USD_TO_CNY", 7.25))
    print(f"汇率 USD→CNY: {usd_to_cny}")

    pricing = await load_pricing()
    print(f"加载定价条目: {len(pricing)} 条")

    # 逐模型统计，避免一次 IN 查询太宽
    total_updated = 0
    affected_dates: set[date] = set()

    async with async_session() as session:
        for model_name, reason in AFFECTED_MODELS.items():
            rows = (
                await session.execute(
                    text(
                        """
                        SELECT id, input_tokens, output_tokens, total_tokens,
                               provider, cost_usd,
                               request_at::date AS day
                        FROM token_usage_logs
                        WHERE model_name = :model
                          AND source NOT LIKE '%estimate%'
                        ORDER BY id
                        """
                    ),
                    {"model": model_name},
                )
            ).fetchall()

            if not rows:
                print(f"[{model_name}]  无记录，跳过")
                continue

            need_update = []
            for r in rows:
                # cost_multiplier 未存入数据库，传 None 让 calc_cost_usd 按
                # GITHUB_COPILOT_COST_MULTIPLIERS 自动查找（provider=copilot 时生效）
                new_cost_usd = calc_cost_usd(
                    pricing,
                    model_name,
                    int(r.input_tokens),
                    int(r.output_tokens),
                    int(r.total_tokens),
                    provider=r.provider,
                    cost_multiplier=None,
                )
                new_cost_cny = round(new_cost_usd * usd_to_cny, 4)
                old_cost_usd = float(r.cost_usd or 0)

                # 只更新有实质性变化的记录（避免浮点噪声）
                if abs(new_cost_usd - old_cost_usd) > 1e-9:
                    need_update.append(
                        {
                            "id": r.id,
                            "cost_usd": new_cost_usd,
                            "cost_cny": new_cost_cny,
                        }
                    )
                    affected_dates.add(r.day)

            delta_usd = sum(d["cost_usd"] for d in need_update) - sum(
                float(r.cost_usd or 0) for r in rows if r.id in {d["id"] for d in need_update}
            )
            print(
                f"[{model_name}]  {reason}\n"
                f"  总记录={len(rows)}  需修正={len(need_update)}  "
                f"  Δcost_usd={delta_usd:+.4f}"
            )

            if not dry_run and need_update:
                # 批量更新
                await session.execute(
                    text(
                        """
                        UPDATE token_usage_logs AS t
                        SET cost_usd = v.cost_usd::numeric,
                            cost_cny = v.cost_cny::numeric
                        FROM (VALUES
                            {placeholders}
                        ) AS v(id, cost_usd, cost_cny)
                        WHERE t.id = v.id::bigint
                        """.format(
                            placeholders=", ".join(
                                f"({d['id']}, {d['cost_usd']}, {d['cost_cny']})"
                                for d in need_update
                            )
                        )
                    )
                )
                total_updated += len(need_update)

        if not dry_run:
            await session.commit()

    print(f"\n{'[DRY-RUN] ' if dry_run else ''}共修正记录: {total_updated}")
    print(f"受影响日期: {sorted(affected_dates)}")

    if not dry_run and affected_dates:
        print("\n重建 daily_usage_summary …")
        for d in sorted(affected_dates):
            await aggregate_daily(d)
            print(f"  rebuilt {d}")
        print("完成。")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dry-run", action="store_true", help="仅统计，不写库")
    args = parser.parse_args()
    asyncio.run(recalc(args.dry_run))


if __name__ == "__main__":
    main()
