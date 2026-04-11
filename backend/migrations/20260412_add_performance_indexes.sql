-- 补充关键索引，提升高频查询性能
-- 执行前无需停服，CREATE INDEX CONCURRENTLY 不会锁表（需在事务外逐条执行）

-- 1. [关键] request_id 去重查询 — 每次 /api/collect 批量入库时都会用到
--    当前无索引，走全表扫描
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_request_id
    ON token_usage_logs (request_id)
    WHERE request_id IS NOT NULL;

-- 2. [高] Tokscale 删除-替换操作的三列组合索引
--    WHERE user_id = ? AND source = 'tokscale' AND request_at BETWEEN ? AND ?
--    现有 idx_usage_user_time 无法跳过非 tokscale 行
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_user_source_time
    ON token_usage_logs (user_id, source, request_at);

-- 3. [中] 告警去重检查 — 每次告警检查循环对每个用户/部门都会查一次
--    WHERE alert_type = ? AND target_type = ? AND target_id = ? AND created_at
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_alerts_type_target
    ON alerts (alert_type, target_type, target_id, created_at);

-- 4. [中] 突增检测 — 覆盖索引，避免回表
--    WHERE date IN (yesterday, day_before) GROUP BY user_id, date SUM(total_tokens)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_daily_date_user_tokens
    ON daily_usage_summary (date, user_id)
    INCLUDE (total_tokens);
