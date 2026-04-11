import logging
import secrets

from fastapi import HTTPException, Request

from app.config import settings

logger = logging.getLogger(__name__)
_api_key_warned = False


async def require_api_key(request: Request) -> None:
    """数据上报接口的 API Key 认证依赖。

    从 X-API-Key 请求头读取 Key 并比对。
    COLLECT_API_KEY 为空时进入迁移宽限期：放行但记录告警日志。
    """
    global _api_key_warned
    if not settings.COLLECT_API_KEY:
        if not _api_key_warned:
            logger.warning("COLLECT_API_KEY 未配置，所有上报请求将被放行（迁移宽限期）")
            _api_key_warned = True
        return

    provided = request.headers.get("X-API-Key", "")
    if not provided or not secrets.compare_digest(provided, settings.COLLECT_API_KEY):
        raise HTTPException(status_code=401, detail="invalid_api_key")


async def require_admin(request: Request) -> None:
    """管理接口的密码认证依赖。

    从 Authorization: Bearer <password> 头读取密码并比对。
    ADMIN_PASSWORD 为空时拒绝所有请求返回 503。
    """
    if not settings.ADMIN_PASSWORD:
        raise HTTPException(status_code=503, detail="admin_not_configured")

    auth = request.headers.get("Authorization", "")
    if not auth.startswith("Bearer "):
        raise HTTPException(status_code=401, detail="unauthorized")

    password = auth[7:]
    if not secrets.compare_digest(password, settings.ADMIN_PASSWORD):
        raise HTTPException(status_code=401, detail="unauthorized")
