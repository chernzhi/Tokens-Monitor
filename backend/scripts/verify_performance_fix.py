#!/usr/bin/env python3
"""
验证性能修复是否成功
用法: python verify_performance_fix.py
"""

import asyncio
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent.parent))

from sqlalchemy import text

from app.config import settings
from app.database import engine


async def verify_fix():
    """验证性能修复"""
    
    print("=" * 70)
    print("验证性能修复")
    print("=" * 70)
    print()
    
    all_passed = True
    
    # 1. 检查连接池配置
    print("📊 [1/3] 检查数据库连接池配置")
    print("-" * 70)
    
    pool_size = engine.pool.size()
    max_overflow = engine.pool._max_overflow
    total_connections = pool_size + max_overflow
    
    print(f"  pool_size: {pool_size}")
    print(f"  max_overflow: {max_overflow}")
    print(f"  总连接数: {total_connections}")
    
    if total_connections >= 100:
        print(f"  ✓ 连接池配置正确（>= 100）")
    else:
        print(f"  ✗ 连接池配置不足（< 100）")
        print(f"    期望: >= 100, 实际: {total_connections}")
        all_passed = False
    
    print()
    
    # 2. 检查索引是否创建
    print("📋 [2/3] 检查数据库索引")
    print("-" * 70)
    
    expected_indexes = [
        "idx_usage_request_at",
        "idx_usage_dept_time",
        "idx_usage_model_time",
        "idx_usage_provider_time",
        "idx_usage_source_app_time",
        "idx_usage_user_time",
        "idx_usage_source_time",
        "idx_usage_user_model_time",
        "idx_usage_dept_model_time",
        "idx_usage_user_time_tokens_cost",
    ]
    
    try:
        async with engine.connect() as conn:
            result = await conn.execute(text("""
                SELECT indexname 
                FROM pg_indexes 
                WHERE tablename = 'token_usage_logs' 
                AND indexname LIKE 'idx_usage_%'
                ORDER BY indexname
            """))
            
            existing_indexes = [row[0] for row in result.fetchall()]
            
            print(f"  已创建索引: {len(existing_indexes)} 个")
            
            missing_indexes = []
            for idx in expected_indexes:
                if idx in existing_indexes:
                    print(f"  ✓ {idx}")
                else:
                    print(f"  ✗ {idx} (缺失)")
                    missing_indexes.append(idx)
            
            if missing_indexes:
                print()
                print(f"  ⚠ 缺少 {len(missing_indexes)} 个索引:")
                for idx in missing_indexes:
                    print(f"    - {idx}")
                print()
                print("  请执行: python scripts/apply_critical_indexes.py")
                all_passed = False
            else:
                print(f"  ✓ 所有索引已创建")
    
    except Exception as e:
        print(f"  ✗ 检查索引失败: {e}")
        all_passed = False
    
    print()
    
    # 3. 测试查询性能
    print("⚡ [3/3] 测试查询性能")
    print("-" * 70)
    
    try:
        async with engine.connect() as conn:
            # 测试查询 1: 按时间范围统计
            import time
            
            start = time.time()
            result = await conn.execute(text("""
                SELECT COUNT(*) 
                FROM token_usage_logs 
                WHERE request_at >= NOW() - INTERVAL '30 days'
            """))
            count = result.scalar()
            elapsed = (time.time() - start) * 1000
            
            print(f"  查询 1: 30天内记录数")
            print(f"    结果: {count:,} 条")
            print(f"    耗时: {elapsed:.2f} ms")
            
            if elapsed < 500:
                print(f"    ✓ 性能良好 (< 500ms)")
            elif elapsed < 1000:
                print(f"    ⚠ 性能一般 (< 1s)")
            else:
                print(f"    ✗ 性能较差 (>= 1s)")
                all_passed = False
            
            print()
            
            # 测试查询 2: 用户排行
            start = time.time()
            result = await conn.execute(text("""
                SELECT 
                    user_id, 
                    SUM(total_tokens) as total_tokens
                FROM token_usage_logs
                WHERE request_at >= NOW() - INTERVAL '30 days'
                  AND user_id IS NOT NULL
                GROUP BY user_id
                ORDER BY total_tokens DESC
                LIMIT 10
            """))
            rows = result.fetchall()
            elapsed = (time.time() - start) * 1000
            
            print(f"  查询 2: 用户排行 TOP 10")
            print(f"    结果: {len(rows)} 条")
            print(f"    耗时: {elapsed:.2f} ms")
            
            if elapsed < 1000:
                print(f"    ✓ 性能良好 (< 1s)")
            elif elapsed < 3000:
                print(f"    ⚠ 性能一般 (< 3s)")
            else:
                print(f"    ✗ 性能较差 (>= 3s)")
                all_passed = False
            
            print()
            
            # 测试查询 3: 查看执行计划
            result = await conn.execute(text("""
                EXPLAIN 
                SELECT COUNT(*) 
                FROM token_usage_logs 
                WHERE request_at >= NOW() - INTERVAL '30 days'
            """))
            
            plan = "\n".join([row[0] for row in result.fetchall()])
            
            print(f"  查询执行计划:")
            for line in plan.split("\n"):
                print(f"    {line}")
            
            if "Index Scan" in plan or "Index Only Scan" in plan:
                print(f"    ✓ 使用索引扫描")
            elif "Bitmap" in plan:
                print(f"    ⚠ 使用位图扫描（可接受）")
            else:
                print(f"    ✗ 未使用索引（全表扫描）")
                all_passed = False
    
    except Exception as e:
        print(f"  ✗ 性能测试失败: {e}")
        all_passed = False
    
    print()
    print("=" * 70)
    
    if all_passed:
        print("✓ 所有检查通过！性能修复成功。")
        print("=" * 70)
        return 0
    else:
        print("✗ 部分检查未通过，请查看上面的详细信息。")
        print("=" * 70)
        return 1


async def main():
    try:
        return await verify_fix()
    finally:
        await engine.dispose()


if __name__ == "__main__":
    exit_code = asyncio.run(main())
    sys.exit(exit_code)
