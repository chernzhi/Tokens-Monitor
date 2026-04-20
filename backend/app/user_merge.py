"""同一自然人可能对应多条 User（邮箱注册工号 vs 本地哈希工号等），统计时按邮箱合并。"""

from sqlalchemy import or_, select
from sqlalchemy.ext.asyncio import AsyncSession

from app.models import User


async def resolve_user_ids_for_personal_stats(db: AsyncSession, user: User) -> list[int]:
    """认证用户 token 统计：合并共享同一 email 的全部活跃用户 id。

    始终把 user.id 自身纳入返回集合——auth_user 可能 is_active=False（被禁用但
    token 仍有效），或 email 字段缺失/历史变更，单纯按 email 过滤会漏掉本人 id，
    导致今日 tokens 显示 0。
    """
    if not user.email:
        return [user.id]
    r = await db.execute(
        select(User.id).where(User.email == user.email, User.is_active == True)  # noqa: E712
    )
    ids = {row[0] for row in r.all()}
    ids.add(user.id)
    return list(ids)


async def resolve_user_ids_from_employee_filter(db: AsyncSession, employee_or_email: str) -> list[int] | None:
    """侧边栏按「当前身份」过滤：参数可为数据库中的 employee_id、或登录后使用的邮箱。

    登录接口返回的 employee_id 字段常为邮箱（见 AuthResponse），与 users.employee_id（数字工号）
    不一致时，必须用 email 列联合匹配，否则会误判用户不存在并返回全零统计。

    历史上同一自然人可能存在多条 User 行（邮箱注册行 + 老的本地哈希工号行 + 把邮箱写到
    employee_id 字段的脏数据行）。必须把所有 seed 命中行 + 所有同 email 的行一并返回，
    否则 r.first() 任选一条若刚好挑中了 email 为空的"脏行"，统计会全 0。
    """
    s = employee_or_email.strip()
    if not s:
        return None
    r = await db.execute(
        select(User.id, User.email).where(
            User.is_active == True,  # noqa: E712
            or_(User.employee_id == s, User.email == s),
        )
    )
    rows = r.all()
    if not rows:
        return None
    ids: set[int] = {row[0] for row in rows}
    emails: set[str] = {row[1] for row in rows if row[1]}
    # 参数本身像邮箱时也纳入扩展，覆盖 seed 行 email 字段为空的情况
    if "@" in s:
        emails.add(s)
    if emails:
        r2 = await db.execute(
            select(User.id).where(User.email.in_(emails), User.is_active == True)  # noqa: E712
        )
        ids.update(row[0] for row in r2.all())
    return list(ids)
