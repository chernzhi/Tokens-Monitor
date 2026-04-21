"""未登录上报别名归并：邮箱 / 32 hex 指纹必须落到已有的真账号上，不能新增僵尸 User。

回归来自现网 users 表里同一自然人出现 2-3 行的故障：
- "10034"（注册行，password_hash 不空、email=wangdong@otw.cn）
- "wangdong@otw.cn"（裸上报新建，无 password）
- "5babbaf...32 hex"（旧客户端用机器指纹当 employee_id）
"""

from __future__ import annotations

import pytest
import pytest_asyncio
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker, create_async_engine

from app.models import Base, Department, User
from app.routers import collect as collect_module


@pytest_asyncio.fixture
async def session():
    engine = create_async_engine("sqlite+aiosqlite:///:memory:", future=True)
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)
    maker = async_sessionmaker(engine, class_=AsyncSession, expire_on_commit=False)
    # 每个测试用例都隔离 module 级缓存，避免跨用例串扰
    collect_module._user_cache.clear()
    collect_module._dept_cache.clear()
    async with maker() as s:
        yield s
    await engine.dispose()


async def _seed_registered_user(
    s: AsyncSession,
    *,
    employee_id: str,
    name: str,
    email: str | None,
    has_password: bool = True,
) -> User:
    u = User(
        employee_id=employee_id,
        name=name,
        email=email,
        password_hash="$2b$12$x" if has_password else None,
        is_active=True,
    )
    s.add(u)
    await s.commit()
    await s.refresh(u)
    return u


@pytest.mark.asyncio
async def test_email_alias_merges_into_registered_account(session: AsyncSession):
    real = await _seed_registered_user(
        session, employee_id="10034", name="王栋", email="wangdong@otw.cn"
    )

    # 未登录客户端把邮箱当 employee_id 上报
    resolved = await collect_module._get_or_create_user(
        session, "wangdong@otw.cn", "王栋", None
    )
    await session.commit()

    assert resolved == real.id, "邮箱别名必须归并到已有注册行"
    # 确认没有新增 User
    from sqlalchemy import func, select
    n = (await session.execute(select(func.count()).select_from(User))).scalar_one()
    assert n == 1, f"users 表应仍只有 1 行，实际 {n}"


@pytest.mark.asyncio
async def test_email_alias_does_not_merge_when_name_mismatches(session: AsyncSession):
    await _seed_registered_user(
        session, employee_id="10001", name="张三", email="shared@otw.cn"
    )

    # 同邮箱但姓名不同：保守起见不归并，落回原 INSERT 行为
    resolved = await collect_module._get_or_create_user(
        session, "shared@otw.cn", "李四", None
    )
    await session.commit()

    from sqlalchemy import select
    user = (await session.execute(select(User).where(User.id == resolved))).scalar_one()
    assert user.employee_id == "shared@otw.cn"
    assert user.name == "李四"


@pytest.mark.asyncio
async def test_machine_fingerprint_merges_when_name_uniquely_matches(session: AsyncSession):
    real = await _seed_registered_user(
        session, employee_id="10029", name="韩佩燕", email="hanpeiyan@otw.cn"
    )

    fingerprint = "5babbaf7e11e8bca63a48d6f8620f523"
    resolved = await collect_module._get_or_create_user(
        session, fingerprint, "韩佩燕", None
    )
    await session.commit()

    assert resolved == real.id


@pytest.mark.asyncio
async def test_machine_fingerprint_does_not_merge_when_name_ambiguous(session: AsyncSession):
    await _seed_registered_user(
        session, employee_id="10010", name="王一博", email="a@otw.cn"
    )
    await _seed_registered_user(
        session, employee_id="10090", name="王一博", email="b@otw.cn"
    )

    fingerprint = "68e6e4841445dc0e1e9893bf1f7a45d1"
    resolved = await collect_module._get_or_create_user(
        session, fingerprint, "王一博", None
    )
    await session.commit()

    # 同名多人 → 不敢归并，回退建一行（保留旧行为，避免误归并）
    from sqlalchemy import select
    user = (await session.execute(select(User).where(User.id == resolved))).scalar_one()
    assert user.employee_id == fingerprint


@pytest.mark.asyncio
async def test_email_alias_prefers_real_employee_id_row_over_email_row(session: AsyncSession):
    # 现网常见状态：先有邮箱形态的脏行（无 password），后注册产生真工号行
    dirty = await _seed_registered_user(
        session,
        employee_id="wangdong@otw.cn",
        name="王栋",
        email=None,
        has_password=False,
    )
    real = await _seed_registered_user(
        session, employee_id="10034", name="王栋", email="wangdong@otw.cn"
    )

    # 此时再来一次邮箱形态的上报必须归到 real，不能命中 dirty
    resolved = await collect_module._get_or_create_user(
        session, "wangdong@otw.cn", "王栋", None
    )
    await session.commit()

    assert resolved == real.id
    assert resolved != dirty.id


@pytest.mark.asyncio
async def test_normal_employee_id_creates_when_absent(session: AsyncSession):
    # 真工号且库里完全没有 → 仍然按原逻辑新建
    resolved = await collect_module._get_or_create_user(
        session, "10099", "新员工", None
    )
    await session.commit()

    from sqlalchemy import select
    user = (await session.execute(select(User).where(User.id == resolved))).scalar_one()
    assert user.employee_id == "10099"
    assert user.name == "新员工"


def test_email_detection():
    assert collect_module._looks_like_email("a@otw.cn")
    assert collect_module._looks_like_email("legacy.45.noemail@merged.local")
    assert not collect_module._looks_like_email("10034")
    assert not collect_module._looks_like_email("5babbaf7e11e8bca63a48d6f8620f523")
    assert not collect_module._looks_like_email("@nodot")


def test_fingerprint_detection():
    assert collect_module._looks_like_machine_fingerprint("5babbaf7e11e8bca63a48d6f8620f523")
    assert not collect_module._looks_like_machine_fingerprint("10034")
    assert not collect_module._looks_like_machine_fingerprint("wangdong@otw.cn")
    assert not collect_module._looks_like_machine_fingerprint("5babbaf7e11e8bca63a48d6f8620f52")  # 31 字符


# ── strict anonymous resolver：邮箱才允许新建，其他 alias 仅在能归并时返回 ──

@pytest.mark.asyncio
async def test_strict_email_alias_creates_placeholder(session: AsyncSession):
    # 邮箱形态：库内尚无注册行 → 允许新建占位 User，等用户后续注册时再自动归并。
    resolved = await collect_module._resolve_anonymous_user_strict(
        session, "newcomer@otw.cn", "新人", None
    )
    await session.commit()
    assert resolved is not None
    from sqlalchemy import select
    user = (await session.execute(select(User).where(User.id == resolved))).scalar_one()
    assert user.employee_id == "newcomer@otw.cn"


@pytest.mark.asyncio
async def test_strict_numeric_employee_id_dropped_when_no_match(session: AsyncSession):
    # 老客户端用 "10099" 这种数字工号上报，库里查不到任何已注册行 → 必须丢弃，不新建 User。
    resolved = await collect_module._resolve_anonymous_user_strict(
        session, "10099", "未知员工", None
    )
    await session.commit()
    assert resolved is None
    from sqlalchemy import func, select
    n = (await session.execute(select(func.count()).select_from(User))).scalar_one()
    assert n == 0, "丢弃匿名上报后 users 表应为空"


@pytest.mark.asyncio
async def test_strict_fingerprint_merges_when_matchable(session: AsyncSession):
    real = await _seed_registered_user(
        session, employee_id="10001", name="张三", email="zhang@otw.cn"
    )
    fp = "abcdef0123456789abcdef0123456789"
    resolved = await collect_module._resolve_anonymous_user_strict(
        session, fp, "张三", None
    )
    await session.commit()
    assert resolved == real.id
    from sqlalchemy import func, select
    n = (await session.execute(select(func.count()).select_from(User))).scalar_one()
    assert n == 1


@pytest.mark.asyncio
async def test_strict_fingerprint_dropped_when_ambiguous(session: AsyncSession):
    await _seed_registered_user(session, employee_id="10001", name="王一博", email="a@otw.cn")
    await _seed_registered_user(session, employee_id="10002", name="王一博", email="b@otw.cn")
    fp = "abcdef0123456789abcdef0123456789"
    resolved = await collect_module._resolve_anonymous_user_strict(
        session, fp, "王一博", None
    )
    await session.commit()
    assert resolved is None
    from sqlalchemy import func, select
    n = (await session.execute(select(func.count()).select_from(User))).scalar_one()
    assert n == 2, "同名多注册用户时不归并、也不新建"


@pytest.mark.asyncio
async def test_strict_desktop_admin_alias_dropped(session: AsyncSession):
    # 现网真实出现过的形态：DESKTOP-0H308LA\\Administrator
    resolved = await collect_module._resolve_anonymous_user_strict(
        session, "DESKTOP-0H308LA\\Administrator", "陈三", None
    )
    await session.commit()
    assert resolved is None

