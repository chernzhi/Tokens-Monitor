# 性能修复指南

## 问题描述

当前系统存在两个性能瓶颈：

1. **数据库连接池过小**：仅30个连接，高并发时会耗尽
2. **缺少关键索引**：大屏查询全表扫描，响应时间 > 5秒

## 快速修复（推荐）

### 一键修复脚本

```bash
cd backend/scripts
./quick_fix.sh
```

脚本会自动完成：
1. 创建数据库索引
2. 提示重启后端服务
3. 验证修复效果

---

## 手动修复

### 步骤 1: 应用数据库索引

**方式 A: 使用 Python 脚本（推荐）**

```bash
cd backend
python scripts/apply_critical_indexes.py
```

**方式 B: 使用 SQL 文件**

```bash
cd backend/migrations
psql -U monitor -d token_monitor -f 20260421_add_critical_indexes.sql
```

**方式 C: Docker 环境**

```bash
# 复制文件到容器
docker cp backend/migrations/20260421_add_critical_indexes.sql token-monitor-db-1:/tmp/

# 执行
docker exec -it token-monitor-db-1 psql -U monitor -d token_monitor -f /tmp/20260421_add_critical_indexes.sql
```

### 步骤 2: 重启后端服务

连接池配置已在代码中修改，需要重启服务生效。

**Docker Compose:**
```bash
docker-compose restart backend
```

**Systemd:**
```bash
systemctl restart token-monitor-backend
```

**手动:**
```bash
# 停止旧进程
pkill -f "uvicorn app.main:app"

# 启动新进程
cd backend
uvicorn app.main:app --host 0.0.0.0 --port 8000
```

### 步骤 3: 验证修复

```bash
cd backend
python scripts/verify_performance_fix.py
```

---

## 修复内容详情

### 1. 连接池扩容

**文件**: `backend/app/database.py`

```python
# 修改前
pool_size=20
max_overflow=10
# 总计: 30 个连接

# 修改后
pool_size=50
max_overflow=50
pool_timeout=30
# 总计: 100 个连接
```

### 2. 新增索引

| 索引名称 | 用途 | 影响查询 |
|---------|------|---------|
| `idx_usage_request_at` | 时间范围查询 | 所有大屏统计 |
| `idx_usage_dept_time` | 部门统计 | 部门排行 |
| `idx_usage_model_time` | 模型统计 | 模型占比 |
| `idx_usage_provider_time` | 供应商统计 | 供应商占比 |
| `idx_usage_source_app_time` | 应用统计 | 应用排行 |
| `idx_usage_user_time` | 用户查询 | 用户详情 |
| `idx_usage_source_time` | 来源统计 | 来源占比 |
| `idx_usage_user_model_time` | 用户模型 | 用户模型详情 |
| `idx_usage_dept_model_time` | 部门模型 | 部门模型详情 |
| `idx_usage_user_time_tokens_cost` | 用户排行 | 覆盖索引优化 |

---

## 预期效果

### 性能对比

| 指标 | 修复前 | 修复后 | 提升 |
|-----|-------|-------|-----|
| 大屏加载时间 | 5-10秒 | < 1秒 | **10倍** |
| 数据库 CPU | 60-80% | 10-20% | **4倍** |
| 并发能力 | 30连接 | 100连接 | **3倍** |
| 查询方式 | 全表扫描 | 索引扫描 | **100倍** |

### 查询性能示例

```sql
-- 30天用户排行查询
-- 修复前: 5000ms (全表扫描)
-- 修复后: 50ms (索引扫描)

SELECT user_id, SUM(total_tokens) 
FROM token_usage_logs
WHERE request_at >= NOW() - INTERVAL '30 days'
GROUP BY user_id
ORDER BY SUM(total_tokens) DESC
LIMIT 100;
```

---

## 注意事项

### 1. 索引创建时间

- 使用 `CREATE INDEX CONCURRENTLY` 不会锁表
- 可在生产环境安全执行
- 每个索引约需 1-5 分钟（取决于数据量）

### 2. 磁盘空间

- 10个索引约需额外 50-100% 的表空间
- 确保磁盘有足够空间（建议预留 10GB）

### 3. PostgreSQL 配置

确保 `max_connections` 足够大：

```sql
-- 查看当前配置
SHOW max_connections;

-- 如果 < 200，需要修改 postgresql.conf
max_connections = 200
```

### 4. 监控建议

添加连接池监控：

```python
@app.get("/health/db")
async def db_health():
    pool = engine.pool
    return {
        "pool_size": pool.size(),
        "checked_in": pool.checkedin(),
        "checked_out": pool.checkedout(),
        "overflow": pool.overflow(),
    }
```

---

## 故障排查

### 问题 1: 索引创建失败

**错误**: `permission denied`

**解决**:
```bash
# 确保使用正确的数据库用户
psql -U monitor -d token_monitor
```

### 问题 2: 连接池未生效

**检查**:
```bash
# 确认服务已重启
docker-compose ps backend

# 查看日志
docker-compose logs backend | grep pool
```

### 问题 3: 查询仍然很慢

**诊断**:
```sql
-- 查看执行计划
EXPLAIN ANALYZE
SELECT COUNT(*) FROM token_usage_logs
WHERE request_at >= NOW() - INTERVAL '30 days';

-- 应该看到 Index Scan，而非 Seq Scan
```

---

## 回滚方案

如需回滚：

### 1. 删除索引

```sql
DROP INDEX CONCURRENTLY IF EXISTS idx_usage_request_at;
DROP INDEX CONCURRENTLY IF EXISTS idx_usage_dept_time;
DROP INDEX CONCURRENTLY IF EXISTS idx_usage_model_time;
DROP INDEX CONCURRENTLY IF EXISTS idx_usage_provider_time;
DROP INDEX CONCURRENTLY IF EXISTS idx_usage_source_app_time;
DROP INDEX CONCURRENTLY IF EXISTS idx_usage_user_time;
DROP INDEX CONCURRENTLY IF EXISTS idx_usage_source_time;
DROP INDEX CONCURRENTLY IF EXISTS idx_usage_user_model_time;
DROP INDEX CONCURRENTLY IF EXISTS idx_usage_dept_model_time;
DROP INDEX CONCURRENTLY IF EXISTS idx_usage_user_time_tokens_cost;
```

### 2. 恢复连接池配置

```python
# backend/app/database.py
engine = create_async_engine(
    settings.DATABASE_URL,
    pool_size=20,
    max_overflow=10,
    pool_recycle=1800,
    pool_pre_ping=True,
)
```

然后重启服务。

---

## 相关文档

- 详细说明: `backend/migrations/README_20260421_CRITICAL_FIX.md`
- 索引 SQL: `backend/migrations/20260421_add_critical_indexes.sql`
- 应用脚本: `backend/scripts/apply_critical_indexes.py`
- 验证脚本: `backend/scripts/verify_performance_fix.py`

---

## 联系支持

如有问题，请：
1. 查看日志: `docker-compose logs backend`
2. 运行验证: `python scripts/verify_performance_fix.py`
3. 联系技术团队
