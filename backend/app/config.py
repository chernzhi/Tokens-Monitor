from pydantic_settings import BaseSettings


class Settings(BaseSettings):
    DATABASE_URL: str = ""
    REDIS_URL: str = "redis://localhost:6379/0"

    # 认证配置
    # 上报用全局 API Key：用于 /api/* 中仍接受「仅 X-API-Key」的接口（如安装向导前 identity-check）；
    # /api/collect、心跳、个人统计 等须登录（Authorization: Bearer），与 COLLECT_API_KEY 无关。
    COLLECT_API_KEY: str = ""  # 为空时 /identity-check 等端点处于宽限期；建议生产显式设置
    ADMIN_PASSWORD: str = ""   # 管理接口密码，为空时拒绝所有管理请求返回 503

    # CORS 允许域名（逗号分隔），为空时使用默认内网地址
    CORS_ALLOWED_ORIGINS: str = ""

    # New API 配置
    NEWAPI_BASE_URL: str = "http://localhost:3001"
    NEWAPI_ADMIN_TOKEN: str = ""

    # 大屏统计、按日聚合使用的时区（与客户端本地日期一致，避免「晚上用的算到前一天」）
    DASHBOARD_TIMEZONE: str = "Asia/Shanghai"

    # 汇率
    USD_TO_CNY: float = 7.25

    # 告警 Webhook（企微/钉钉/飞书）
    ALERT_WEBHOOK_URL: str = ""

    # 同步间隔（分钟）
    SYNC_INTERVAL_MINUTES: int = 10

    # 扩展分发目录
    EXTENSION_DIR: str = "/opt/token-monitor/extensions"

    # Tokscale 等上报失败时是否在 API 响应中带简短错误信息（便于排障；生产可关）
    EXPOSE_INTERNAL_ERRORS: bool = False

    model_config = {"env_file": ".env"}


settings = Settings()
