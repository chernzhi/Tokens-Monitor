-- 补充关键索引，修复大屏查询性能问题
-- 执行前无需停服，CREATE INDEX CONCURRENTLY 不会锁表（需在事务外逐条执行）
-- 执行方式：psql -U monitor -d token_monitor -f 20260421_add_critical_indexes.sql

-- 1. [关键] 按时间范围查询 — 大屏所有统计接口都会用到
--    WHERE request_at >= ? AND request_at <= ?
--    当前无此索引，导致全表扫描
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_request_at
    ON token_usage_logs (request_at DESC);

-- 2. [关键] 按部门统计 — 部门排行、部门详情
--    WHERE department_id = ? AND request_at >= ?
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_dept_time
    ON token_usage_logs (department_id, request_at DESC)
    WHERE department_id IS NOT NULL;

-- 3. [关键] 按模型统计 — 模型占比、模型排行
--    WHERE model_name = ? AND request_at >= ?
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_model_time
    ON token_usage_logs (model_name, request_at DESC);

-- 4. [关键] 按供应商统计 — 供应商占比
--    WHERE provider = ? AND request_at >= ?
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_provider_time
    ON token_usage_logs (provider, request_at DESC);

-- 5. [高] 按来源应用统计 — 应用来源排行
--    WHERE source_app = ? AND request_at >= ?
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_source_app_time
    ON token_usage_logs (source_app, request_at DESC)
    WHERE source_app IS NOT NULL;

-- 6. [高] 按用户和时间查询 — 用户详情、个人统计
--    WHERE user_id = ? AND request_at >= ?
--    已有 idx_usage_user_source_time，但可以优化为更通用的索引
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_user_time
    ON token_usage_logs (user_id, request_at DESC)
    WHERE user_id IS NOT NULL;

-- 7. [中] 按来源类型统计 — 区分 client/gateway/tokscale/estimate
--    WHERE source = ? AND request_at >= ?
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_source_time
    ON token_usage_logs (source, request_at DESC);

-- 8. [中] 复合索引优化 — 用户+模型+时间（用户模型使用详情）
--    WHERE user_id = ? AND model_name = ? AND request_at >= ?
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_user_model_time
    ON token_usage_logs (user_id, model_name, request_at DESC)
    WHERE user_id IS NOT NULL;

-- 9. [中] 复合索引优化 — 部门+模型+时间（部门模型使用详情）
--    WHERE department_id = ? AND model_name = ? AND request_at >= ?
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_dept_model_time
    ON token_usage_logs (department_id, model_name, request_at DESC)
    WHERE department_id IS NOT NULL;

-- 10. [低] 覆盖索引 — 用户排行查询优化（避免回表）
--     SELECT user_id, SUM(total_tokens), SUM(cost_cny) WHERE request_at >= ? GROUP BY user_id
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_user_time_tokens_cost
    ON token_usage_logs (user_id, request_at DESC)
    INCLUDE (total_tokens, cost_cny, request_count)
    WHERE user_id IS NOT NULL;

-- 验证索引创建情况
-- SELECT schemaname, tablename, indexname, indexdef 
-- FROM pg_indexes 
-- WHERE tablename = 'token_usage_logs' 
-- ORDER BY indexname;
