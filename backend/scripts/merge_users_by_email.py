#!/usr/bin/env python3
"""
按「同 email」合并 users 表里的多条记录。

背景：早期 collect 在客户端裸上报时，会按 rec.user_id（邮箱 / 工号 / 32hex 机器指纹 /
DESKTOP-xxx\\Administrator）当 employee_id 直接 INSERT，同一个真实员工经常被拆成 2~3 行。

策略：
  - 仅处理 email 非空且去除空白后一致的分组。
  - 同组内按以下优先级选 canonical：
      1) 有 password_hash（即真实注册过）
      2) employee_id 不像邮箱、不像 32hex 机器指纹（多半是真员工号 e.g. "10034"）
      3) 创建时间最早 / id 最小
  - 其余行作为 src 调 merge_one_user_into 迁移 token_usage_logs / daily_usage_summary /
    alerts / clients 后删除。
  - 默认 dry-run，需要传 --execute 才落库。

用法：
  DATABASE_URL=postgresql+asyncpg://... python scripts/merge_users_by_email.py --dry-run
  DATABASE_URL=...                     python scripts/merge_users_by_email.py --execute
"""

from __future__ import annotations

import argparse
import asyncio
import os
import re
import sys
from collections import defaultdict
from pathlib import Path

_ROOT = Path(__file__).resolve().parent.parent
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from sqlalchemy import func, select
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker, create_async_engine

from app.models import User
from scripts.merge_duplicate_users_by_name import merge_one_user_into  # type: ignore


_HEX_FINGERPRINT_RE = re.compile(r"^[0-9a-f]{32}$")


def _looks_like_email(value: str | None) -> bool:
    if not value or "@" not in value:
        return False
    domain = value.rsplit("@", 1)[-1]
    return "." in domain and len(domain) >= 3


def _looks_like_machine_fingerprint(value: str | None) -> bool:
    return bool(_HEX_FINGERPRINT_RE.match((value or "").strip()))


def _pick_canonical(group: list[User]) -> User:
    """从同 email 分组中挑出 canonical 用户。"""
    def score(u: User) -> tuple:
        has_password = bool((u.password_hash or "").strip())
        eid = (u.employee_id or "").strip()
        is_real_eid = (
            bool(eid)
            and not _looks_like_email(eid)
            and not _looks_like_machine_fingerprint(eid)
            and "\\" not in eid  # DESKTOP-xxx\Administrator
        )
        # 元组按字典序比较：True > False，所以用负值让"有"排前面
        return (
            -int(has_password),
            -int(is_real_eid),
            u.id,  # 最后用 id 兜底，越小越靠前
        )

    return sorted(group, key=score)[0]


async def run(dry_run: bool) -> None:
    db_url = os.environ.get("DATABASE_URL")
    if not db_url:
        print("缺少环境变量 DATABASE_URL", file=sys.stderr)
        sys.exit(1)

    engine = create_async_engine(db_url, echo=False)
    Session = async_sessionmaker(engine, class_=AsyncSession, expire_on_commit=False)

    async with Session() as session:
        rows = (
            await session.execute(
                select(User).where(User.email.is_not(None), func.trim(User.email) != "")
            )
        ).scalars().all()

        groups: dict[str, list[User]] = defaultdict(list)
        for u in rows:
            key = (u.email or "").strip().casefold()
            if key:
                groups[key].append(u)

        total_merged = 0
        total_groups_touched = 0
        for email, group in sorted(groups.items()):
            if len(group) < 2:
                continue
            canonical = _pick_canonical(group)
            others = [u for u in group if u.id != canonical.id]
            total_groups_touched += 1
            print(
                f"[{email}] 保留 id={canonical.id} eid={canonical.employee_id!r} "
                f"name={canonical.name!r} ← 合并 {[(u.id, u.employee_id) for u in others]}"
            )
            for src in others:
                stat = await merge_one_user_into(session, src, canonical, dry_run=dry_run)
                print(f"    merge {stat}")
                total_merged += 1

        if dry_run:
            await session.rollback()
            print(f"\n[DRY-RUN] 涉及 {total_groups_touched} 个邮箱分组，将合并 {total_merged} 条记录")
        else:
            await session.commit()
            print(f"\n[EXECUTED] 已合并 {total_merged} 条记录，涉及 {total_groups_touched} 个邮箱分组")

    await engine.dispose()


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser()
    grp = p.add_mutually_exclusive_group(required=True)
    grp.add_argument("--dry-run", action="store_true")
    grp.add_argument("--execute", action="store_true")
    return p.parse_args()


if __name__ == "__main__":
    args = parse_args()
    asyncio.run(run(dry_run=args.dry_run))
