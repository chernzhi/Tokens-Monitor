#!/usr/bin/env python3
"""
按「同名」合并用户：优先保留有邮箱的账号，将其余同名账号的 token / 日汇总 / 告警 / 客户端记录迁入后删除源用户。

最后：删除仍无邮箱且无使用数据的僵尸行；若仅有无邮箱账号但仍有用量，为其写入占位邮箱以免误删数据。

用法（在 backend 目录或设置 PYTHONPATH）:
  DATABASE_URL=postgresql+asyncpg://... python scripts/merge_duplicate_users_by_name.py --dry-run
  DATABASE_URL=... python scripts/merge_duplicate_users_by_name.py --execute
"""

from __future__ import annotations

import argparse
import asyncio
import os
import sys
from pathlib import Path

# 保证可导入 app
_ROOT = Path(__file__).resolve().parent.parent
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from sqlalchemy import delete, exists, func, or_, select, update
from sqlalchemy.ext.asyncio import AsyncSession, create_async_engine
from sqlalchemy.orm import sessionmaker

from app.models import Alert, Client, DailyUsageSummary, TokenUsageLog, User


def norm_name(name: str | None) -> str:
    """与 collect 侧人名归一化一致：去空白折叠。"""
    return "".join((name or "").split()).casefold()


async def merge_daily_for_pair(session: AsyncSession, src_id: int, tgt_id: int) -> int:
    """将 src 的 daily_usage_summary 合并进 tgt，冲突键则累加后删源行。"""
    rows = (
        await session.execute(select(DailyUsageSummary).where(DailyUsageSummary.user_id == src_id))
    ).scalars().all()
    touched = 0
    for r in rows:
        existing = (
            await session.execute(
                select(DailyUsageSummary).where(
                    DailyUsageSummary.user_id == tgt_id,
                    DailyUsageSummary.date == r.date,
                    DailyUsageSummary.proj_key == r.proj_key,
                    DailyUsageSummary.model_name == r.model_name,
                    DailyUsageSummary.provider == r.provider,
                    DailyUsageSummary.dept_key == r.dept_key,
                )
            )
        ).scalar_one_or_none()
        if existing:
            existing.total_requests = int(existing.total_requests or 0) + int(r.total_requests or 0)
            existing.input_tokens = int(existing.input_tokens or 0) + int(r.input_tokens or 0)
            existing.output_tokens = int(existing.output_tokens or 0) + int(r.output_tokens or 0)
            existing.total_tokens = int(existing.total_tokens or 0) + int(r.total_tokens or 0)
            existing.cost_usd = float(existing.cost_usd or 0) + float(r.cost_usd or 0)
            existing.cost_cny = float(existing.cost_cny or 0) + float(r.cost_cny or 0)
            await session.delete(r)
        else:
            r.user_id = tgt_id
        touched += 1
    return touched


async def merge_one_user_into(
    session: AsyncSession,
    src: User,
    tgt: User,
    *,
    dry_run: bool,
) -> dict:
    if src.id == tgt.id:
        return {"skipped": True}

    out = {"src_id": src.id, "tgt_id": tgt.id, "logs": 0, "alerts": 0, "clients": 0, "daily_merged": 0}

    log_count = (
        await session.execute(
            select(func.count()).select_from(TokenUsageLog).where(TokenUsageLog.user_id == src.id)
        )
    ).scalar_one()
    out["logs"] = int(log_count or 0)

    if dry_run:
        return out

    await session.execute(
        update(TokenUsageLog).where(TokenUsageLog.user_id == src.id).values(user_id=tgt.id)
    )

    out["daily_merged"] = await merge_daily_for_pair(session, src.id, tgt.id)

    ar = await session.execute(
        update(Alert)
        .where(Alert.target_type == "user", Alert.target_id == src.id)
        .values(target_id=tgt.id)
    )
    out["alerts"] = ar.rowcount or 0

    cr = await session.execute(
        update(Client).where(Client.user_id == src.employee_id).values(user_id=tgt.employee_id)
    )
    out["clients"] = cr.rowcount or 0

    await session.execute(delete(User).where(User.id == src.id))
    return out


async def run_merge(dry_run: bool) -> None:
    db_url = os.environ.get("DATABASE_URL")
    if not db_url:
        print("缺少环境变量 DATABASE_URL", file=sys.stderr)
        sys.exit(1)

    engine = create_async_engine(db_url, echo=False)
    async_session = sessionmaker(engine, class_=AsyncSession, expire_on_commit=False)

    async with async_session() as session:
        result = await session.execute(select(User).order_by(User.id))
        users = result.scalars().all()

        by_name: dict[str, list[User]] = {}
        for u in users:
            key = norm_name(u.name)
            if key not in by_name:
                by_name[key] = []
            by_name[key].append(u)

        merges: list[tuple[User, list[User]]] = []
        for _key, group in by_name.items():
            if len(group) < 2:
                continue
            with_email = [u for u in group if (u.email or "").strip()]
            if with_email:
                target = min(with_email, key=lambda u: u.id)
                others = [u for u in group if u.id != target.id]
            else:
                target = min(group, key=lambda u: u.id)
                others = [u for u in group if u.id != target.id]
            merges.append((target, others))

        print(f"发现 {len(merges)} 个同名组需要合并（每组保留 1 人，合并 {sum(len(o) for _, o in merges)} 个源账号）")
        if dry_run:
            for tgt, others in merges:
                print(f"  保留 id={tgt.id} email={tgt.email!r} name={tgt.name!r} ← 合并 {[u.id for u in others]}")
            await session.rollback()
            return

        total = []
        for tgt, others in merges:
            for src in others:
                info = await merge_one_user_into(session, src, tgt, dry_run=False)
                total.append(info)
                print(f"  已合并 src={info['src_id']} → tgt={info['tgt_id']} logs={info['logs']}")

        # 占位邮箱：仍有用量但无邮箱的孤立账号（同名已处理完仍单独存在）
        orphan = (
            await session.execute(
                select(User).where(
                    (User.email.is_(None)) | (func.trim(User.email) == ""),
                )
            )
        ).scalars().all()

        for u in orphan:
            has_logs = (
                await session.execute(
                    select(func.count()).select_from(TokenUsageLog).where(TokenUsageLog.user_id == u.id)
                )
            ).scalar_one()
            has_daily = (
                await session.execute(
                    select(func.count())
                    .select_from(DailyUsageSummary)
                    .where(DailyUsageSummary.user_id == u.id)
                )
            ).scalar_one()
            if int(has_logs or 0) + int(has_daily or 0) > 0:
                u.email = f"legacy.{u.id}.noemail@merged.local"
                print(f"  占位邮箱: id={u.id} → {u.email}")

        # 删除：无邮箱且无用量（僵尸行）
        zombie_ids = (
            (
                await session.execute(
                    select(User.id).where(
                        or_(User.email.is_(None), func.trim(User.email) == ""),
                        ~exists(
                            select(TokenUsageLog.id).where(TokenUsageLog.user_id == User.id)
                        ),
                        ~exists(
                            select(DailyUsageSummary.id).where(DailyUsageSummary.user_id == User.id)
                        ),
                    )
                )
            )
            .scalars()
            .all()
        )
        for zid in zombie_ids:
            await session.execute(delete(User).where(User.id == zid))
            print(f"  已删除无邮箱无数据用户 id={zid}")

        await session.commit()
        print(f"完成：合并操作 {len(total)} 次。")

    await engine.dispose()


async def run_pair_merge(source_eid: str, target_eid: str, dry_run: bool) -> None:
    """将源用户（employee_id）合并到目标用户，删除源行。"""
    db_url = os.environ.get("DATABASE_URL")
    if not db_url:
        print("缺少环境变量 DATABASE_URL", file=sys.stderr)
        sys.exit(1)

    engine = create_async_engine(db_url, echo=False)
    async_session = sessionmaker(engine, class_=AsyncSession, expire_on_commit=False)

    async with async_session() as session:
        src = (
            await session.execute(select(User).where(User.employee_id == source_eid.strip()))
        ).scalar_one_or_none()
        tgt = (
            await session.execute(select(User).where(User.employee_id == target_eid.strip()))
        ).scalar_one_or_none()
        if not src:
            print(f"未找到源用户 employee_id={source_eid!r}", file=sys.stderr)
            sys.exit(1)
        if not tgt:
            print(f"未找到目标用户 employee_id={target_eid!r}", file=sys.stderr)
            sys.exit(1)
        print(f"源: id={src.id} employee_id={src.employee_id!r} name={src.name!r} email={src.email!r}")
        print(f"目标: id={tgt.id} employee_id={tgt.employee_id!r} name={tgt.name!r} email={tgt.email!r}")
        if src.id == tgt.id:
            print("源与目标为同一用户，无需操作。")
            await session.rollback()
            return

        info = await merge_one_user_into(session, src, tgt, dry_run=dry_run)
        if dry_run:
            print(f"[dry-run] 将迁移 token 日志约 {info.get('logs', 0)} 条")
            await session.rollback()
        else:
            await session.commit()
            print(f"完成: src_id={info.get('src_id')} → tgt_id={info.get('tgt_id')} logs={info.get('logs')} clients={info.get('clients')}")

    await engine.dispose()


def main() -> None:
    p = argparse.ArgumentParser()
    p.add_argument("--dry-run", action="store_true", help="只打印计划不写入")
    p.add_argument("--execute", action="store_true", help="执行合并与清理")
    p.add_argument(
        "--source-employee-id",
        help="与 --target-employee-id 合用：按工号将源用户并入目标用户",
    )
    p.add_argument("--target-employee-id", help="保留的目标用户工号")
    args = p.parse_args()

    if args.source_employee_id and args.target_employee_id:
        if not args.execute and not args.dry_run:
            print("请指定 --dry-run 或 --execute", file=sys.stderr)
            sys.exit(2)
        asyncio.run(
            run_pair_merge(
                args.source_employee_id,
                args.target_employee_id,
                dry_run=args.dry_run,
            )
        )
        return

    if not args.execute and not args.dry_run:
        print("请指定 --dry-run 或 --execute", file=sys.stderr)
        sys.exit(2)
    asyncio.run(run_merge(dry_run=args.dry_run))


if __name__ == "__main__":
    main()
