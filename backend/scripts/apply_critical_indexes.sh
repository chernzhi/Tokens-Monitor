#!/bin/bash
# 应用关键索引到数据库
# 用法: ./apply_critical_indexes.sh

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}应用关键数据库索引${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

# 检查环境变量
if [ -z "$DATABASE_URL" ]; then
    echo -e "${RED}错误: DATABASE_URL 环境变量未设置${NC}"
    echo "请设置 DATABASE_URL，例如："
    echo "export DATABASE_URL='postgresql://monitor:password@localhost:5432/token_monitor'"
    exit 1
fi

# 解析 DATABASE_URL
# 格式: postgresql://user:password@host:port/database
DB_USER=$(echo $DATABASE_URL | sed -n 's/.*:\/\/\([^:]*\):.*/\1/p')
DB_PASS=$(echo $DATABASE_URL | sed -n 's/.*:\/\/[^:]*:\([^@]*\)@.*/\1/p')
DB_HOST=$(echo $DATABASE_URL | sed -n 's/.*@\([^:]*\):.*/\1/p')
DB_PORT=$(echo $DATABASE_URL | sed -n 's/.*:\([0-9]*\)\/.*/\1/p')
DB_NAME=$(echo $DATABASE_URL | sed -n 's/.*\/\([^?]*\).*/\1/p')

echo -e "${YELLOW}数据库信息:${NC}"
echo "  主机: $DB_HOST"
echo "  端口: $DB_PORT"
echo "  数据库: $DB_NAME"
echo "  用户: $DB_USER"
echo ""

# 迁移文件路径
MIGRATION_FILE="../migrations/20260421_add_critical_indexes.sql"

if [ ! -f "$MIGRATION_FILE" ]; then
    echo -e "${RED}错误: 找不到迁移文件 $MIGRATION_FILE${NC}"
    exit 1
fi

echo -e "${YELLOW}开始应用索引...${NC}"
echo "注意: CREATE INDEX CONCURRENTLY 不会锁表，可以在生产环境安全执行"
echo ""

# 设置 PGPASSWORD 环境变量
export PGPASSWORD="$DB_PASS"

# 执行迁移
# 注意: CONCURRENTLY 索引不能在事务中创建，所以逐条执行
while IFS= read -r line; do
    # 跳过注释和空行
    if [[ "$line" =~ ^[[:space:]]*-- ]] || [[ -z "$line" ]]; then
        continue
    fi
    
    # 如果是 CREATE INDEX 语句，执行它
    if [[ "$line" =~ CREATE[[:space:]]+INDEX ]]; then
        echo -e "${YELLOW}执行: ${line:0:80}...${NC}"
        echo "$line" | psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1
        
        if [ $? -eq 0 ]; then
            echo -e "${GREEN}✓ 成功${NC}"
        else
            echo -e "${RED}✗ 失败${NC}"
            exit 1
        fi
        echo ""
    fi
done < "$MIGRATION_FILE"

# 清除密码
unset PGPASSWORD

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}索引应用完成！${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo "验证索引创建情况："
echo "psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME -c \"SELECT indexname FROM pg_indexes WHERE tablename = 'token_usage_logs' ORDER BY indexname;\""
