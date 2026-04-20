"""同一自然人可能对应多条 User（邮箱注册工号 vs 本地哈希工号等），统计时按邮箱合并。"""

from sqlalchemy import or_, select
from sqlalchemy.ext.asyncio import AsyncSession

from app.models import User


async def resolve_user_ids_for_personal_stats(db: AsyncSession, user: User) -> list[int]:
    """认证用户 token 统计：合并共享同一 email 的全部活跃用户 id。"""
    if not user.email:
        return [user.id]
    r = await db.execute(
        select(User.id).where(User.email == user.email, User.is_active == True)  # noqa: E712
    )
    ids = list({row[0] for row in r.all()})
    return ids if ids else [user.id]


async def resolve_user_ids_from_employee_filter(db: AsyncSession, employee_or_email: str) -> list[int] | None:
    """侧边栏按「当前身份」过滤：参数可为数据库中的 employee_id、或登录后使用的邮箱。

    登录接口返回的 employee_id 字段常为邮箱（见 AuthResponse），与 users.employee_id（数字工号）
    不一致时，必须用 email 列联合匹配，否则会误判用户不存在并返回全零统计。
    """
    s = employee_or_email.strip()
    if not s:
        return None
    r = await db.execute(
        select(User.id).where(
            User.is_active == True,  # noqa: E712
            or_(User.employee_id == s, User.email == s),
        )
    )
    row = r.first()
    if row is None:
        return None
    uid = row[0]
    u = await db.get(User, uid)
    if u and u.email:
        r2 = await db.execute(
            select(User.id).where(User.email == u.email, User.is_active == True)  # noqa: E712
        )
        return list({x[0] for x in r2.all()})
    return [uid]
