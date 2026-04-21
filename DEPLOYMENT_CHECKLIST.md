# 性能修复部署检查清单

## 📋 部署前检查

- [ ] 已阅读 `PERFORMANCE_FIX_GUIDE.md`
- [ ] 已阅读 `FIXES_SUMMARY.md`
- [ ] 已备份数据库（可选但推荐）
- [ ] 确认有足够的磁盘空间（至少 10GB）
- [ ] 确认 PostgreSQL `max_connections >= 200`
- [ ] 已通知团队即将进行维护

---

## 🚀 部署步骤

### 方式 A: 一键部署（推荐）

```bash
cd backend/scripts
./quick_fix.sh
```

- [ ] 脚本执行成功
- [ ] 索引创建完成
- [ ] 服务已重启
- [ ] 验证通过

---

### 方式 B: 手动部署

#### 步骤 1: 应用数据库索引

```bash
cd backend
python scripts/apply_critical_indexes.py
```

**检查点**:
- [ ] 脚本输出显示 "成功创建: 10"
- [ ] 没有错误信息
- [ ] 所有索引状态为 "✓ 成功" 或 "⊙ 已存在"

**预计耗时**: 10-30 分钟

---

#### 步骤 2: 重启后端服务

**Docker Compose**:
```bash
docker-compose restart backend
```

**Systemd**:
```bash
systemctl restart token-monitor-backend
```

**检查点**:
- [ ] 服务成功重启
- [ ] 日志无错误: `docker-compose logs backend | tail -50`
- [ ] 健康检查通过: `curl http://localhost:8000/health`

**预计停机时间**: < 10 秒

---

#### 步骤 3: 验证修复

```bash
cd backend
python scripts/verify_performance_fix.py
```

**检查点**:
- [ ] 连接池配置正确（>= 100）
- [ ] 所有索引已创建（10个）
- [ ] 查询性能良好（< 1秒）
- [ ] 使用索引扫描（非全表扫描）

---

## ✅ 部署后验证

### 1. 功能验证

- [ ] 访问大屏: http://your-server:3080
- [ ] 大屏加载时间 < 1秒
- [ ] 所有图表正常显示
- [ ] 数据统计正确

### 2. 性能验证

```bash
# 查看数据库 CPU
top -p $(pgrep postgres)

# 查看连接数
psql -U monitor -d token_monitor -c "SELECT count(*) FROM pg_stat_activity;"

# 查看慢查询
psql -U monitor -d token_monitor -c "
SELECT query, mean_time 
FROM pg_stat_statements 
WHERE query LIKE '%token_usage_logs%' 
ORDER BY mean_time DESC 
LIMIT 5;"
```

**检查点**:
- [ ] 数据库 CPU < 30%
- [ ] 活跃连接数 < 50
- [ ] 平均查询时间 < 100ms

### 3. 监控验证

```bash
# 连接池状态
curl http://localhost:8000/health/db

# 应该看到:
# {
#   "pool_size": 50,
#   "checked_in": 45,
#   "checked_out": 5,
#   "overflow": 0,
#   "total": 50
# }
```

**检查点**:
- [ ] `checked_out` < 30（连接使用率 < 60%）
- [ ] `overflow` = 0（无溢出连接）

---

## 📊 性能对比记录

### 修复前（记录基线）

| 指标 | 数值 |
|-----|-----|
| 大屏加载时间 | _____ 秒 |
| 数据库 CPU | _____ % |
| 活跃连接数 | _____ |
| 查询平均时间 | _____ ms |

### 修复后（记录结果）

| 指标 | 数值 |
|-----|-----|
| 大屏加载时间 | _____ 秒 |
| 数据库 CPU | _____ % |
| 活跃连接数 | _____ |
| 查询平均时间 | _____ ms |

---

## 🔍 故障排查

### 问题 1: 索引创建失败

**症状**: `permission denied` 或 `disk full`

**排查**:
```bash
# 检查权限
psql -U monitor -d token_monitor -c "\du"

# 检查磁盘空间
df -h

# 检查现有索引
psql -U monitor -d token_monitor -c "
SELECT indexname 
FROM pg_indexes 
WHERE tablename = 'token_usage_logs';"
```

**解决**:
- 确保使用正确的数据库用户
- 清理磁盘空间
- 手动创建缺失的索引

---

### 问题 2: 服务重启失败

**症状**: 服务无法启动或立即退出

**排查**:
```bash
# 查看日志
docker-compose logs backend --tail=100

# 检查配置
docker-compose config

# 检查端口占用
netstat -tlnp | grep 8000
```

**解决**:
- 检查配置文件语法
- 确保端口未被占用
- 检查数据库连接

---

### 问题 3: 查询仍然慢

**症状**: 大屏加载时间 > 3秒

**排查**:
```sql
-- 查看执行计划
EXPLAIN ANALYZE
SELECT COUNT(*) 
FROM token_usage_logs
WHERE request_at >= NOW() - INTERVAL '30 days';

-- 查看索引使用情况
SELECT 
    schemaname,
    tablename,
    indexname,
    idx_scan,
    idx_tup_read,
    idx_tup_fetch
FROM pg_stat_user_indexes
WHERE tablename = 'token_usage_logs'
ORDER BY idx_scan DESC;
```

**解决**:
- 确认索引已创建
- 运行 `ANALYZE token_usage_logs;` 更新统计信息
- 检查是否有其他慢查询

---

## 🔄 回滚计划

如果修复导致问题，可以回滚：

### 1. 回滚索引

```sql
-- 删除新创建的索引
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

### 2. 回滚连接池配置

```bash
# 恢复旧版本代码
git checkout HEAD~1 backend/app/database.py

# 重启服务
docker-compose restart backend
```

---

## 📝 部署记录

**部署日期**: _______________  
**部署人员**: _______________  
**部署环境**: [ ] 生产 [ ] 测试  
**部署方式**: [ ] 一键 [ ] 手动  

**部署结果**:
- [ ] 成功
- [ ] 部分成功（说明原因）: _______________
- [ ] 失败（说明原因）: _______________

**遇到的问题**: _______________

**解决方案**: _______________

**备注**: _______________

---

## 📞 联系方式

**技术支持**: _______________  
**紧急联系**: _______________  

---

## 📚 相关文档

- [性能修复指南](PERFORMANCE_FIX_GUIDE.md)
- [修复总结](FIXES_SUMMARY.md)
- [详细说明](backend/migrations/README_20260421_CRITICAL_FIX.md)
- [索引 SQL](backend/migrations/20260421_add_critical_indexes.sql)

---

**检查清单版本**: 1.0  
**最后更新**: 2026-04-21
