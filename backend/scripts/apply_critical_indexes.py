#!/usr/bin/env python3
"""
应用关键数据库索引
用法: python apply_critical_indexes.py
"""

import asyncio
import os
import sys
from pathlib import Path

# 添加 app 到路径
sys.path.insert(0, str(Path(__file__).parent.parent))

from sqlalchemy import text
from sqlalchemy.ext.asyncio import create_async_engine

from app.config import settings


async def apply_indexes():
    """应用关键索引到数据库"""
    
    print("=" * 60)
    print("应用关键数据库索引")
    print("=" * 60)
    print()
    
    if not settings.DATABASE_URL:
        print("❌ 错误: DATABASE_URL 未配置")
        print("请在 .env 文件或环境变量中设置 DATABASE_URL")
        return 1
    
    print(f"📊 数据库: {settings.DATABASE_URL.split('@')[-1]}")
    print()
    
    # 读取迁移文件
    migration_file = Path(__file__).parent.parent / "migrations" / "20260421_add_critical_indexes.sql"
    
    if not migration_file.exists():
        print(f"❌ 错误: 找不到迁移文件 {migration_file}")
        return 1
    
    print(f"📄 读取迁移文件: {migration_file.name}")
    sql_content = migration_file.read_text(encoding="utf-8")
    
    # 提取所有 CREATE INDEX 语句
    statements = []
    current_statement = []
    
    for line in sql_content.split("\n"):
        stripped = line.strip()
        
        # 跳过注释和空行
        if not stripped or stripped.startswith("--"):
            continue
        
        current_statement.append(line)
        
        # 如果行以分号结束，说明语句完整
        if stripped.endswith(";"):
            full_statement = "\n".join(current_statement).strip()
            if "CREATE INDEX" in full_statement.upper():
                statements.append(full_statement)
            current_statement = []
    
    print(f"✓ 找到 {len(statements)} 个索引创建语句")
    print()
    
    # 创建数据库引擎（不使用连接池，因为是一次性操作）
    engine = create_async_engine(
        settings.DATABASE_URL,
        pool_size=1,
        max_overflow=0,
        echo=False,
    )
    
    try:
        async with engine.begin() as conn:
            # 注意: CONCURRENTLY 索引必须在事务外执行
            # 所以我们需要使用 autocommit 模式
            await conn.execution_options(isolation_level="AUTOCOMMIT")
            
            success_count = 0
            skip_count = 0
            
            for i, statement in enumerate(statements, 1):
                # 提取索引名称
                index_name = "unknown"
                if "IF NOT EXISTS" in statement:
                    parts = statement.split("IF NOT EXISTS")
                    if len(parts) > 1:
                        index_name = parts[1].split()[0].strip()
                
                print(f"[{i}/{len(statements)}] 创建索引: {index_name}")
                
                try:
                    # 执行索引创建
                    await conn.execute(text(statement))
                    print(f"  ✓ 成功")
                    success_count += 1
                    
                except Exception as e:
                    error_msg = str(e)
                    
                    # 如果索引已存在，不算错误
                    if "already exists" in error_msg.lower():
                        print(f"  ⊙ 已存在（跳过）")
                        skip_count += 1
                    else:
                        print(f"  ✗ 失败: {error_msg}")
                        # 继续执行其他索引
                
                print()
        
        print("=" * 60)
        print(f"✓ 索引应用完成")
        print(f"  成功创建: {success_count}")
        print(f"  已存在跳过: {skip_count}")
        print(f"  总计: {len(statements)}")
        print("=" * 60)
        print()
        
        # 验证索引
        print("📋 验证索引创建情况...")
        async with engine.connect() as conn:
            result = await conn.execute(text("""
                SELECT indexname, indexdef 
                FROM pg_indexes 
                WHERE tablename = 'token_usage_logs' 
                AND indexname LIKE 'idx_usage_%'
                ORDER BY indexname
            """))
            
            indexes = result.fetchall()
            print(f"✓ token_usage_logs 表共有 {len(indexes)} 个索引")
            for idx_name, idx_def in indexes:
                print(f"  • {idx_name}")
        
        return 0
        
    except Exception as e:
        print(f"❌ 执行失败: {e}")
        return 1
        
    finally:
        await engine.dispose()


if __name__ == "__main__":
    exit_code = asyncio.run(apply_indexes())
    sys.exit(exit_code)
