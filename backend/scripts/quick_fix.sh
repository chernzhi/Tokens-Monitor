#!/bin/bash
# 一键应用性能修复
# 用法: ./quick_fix.sh

set -e

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}AI Token 监控 - 性能修复${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

# 检查是否在正确的目录
if [ ! -f "apply_critical_indexes.py" ]; then
    echo -e "${RED}错误: 请在 backend/scripts 目录下运行此脚本${NC}"
    exit 1
fi

# 步骤 1: 应用数据库索引
echo -e "${YELLOW}步骤 1/3: 应用数据库索引${NC}"
echo "----------------------------------------"
python apply_critical_indexes.py
if [ $? -ne 0 ]; then
    echo -e "${RED}✗ 索引应用失败${NC}"
    exit 1
fi
echo ""

# 步骤 2: 重启后端服务（应用连接池配置）
echo -e "${YELLOW}步骤 2/3: 重启后端服务${NC}"
echo "----------------------------------------"
echo "连接池配置已更新，需要重启后端服务生效"
echo ""
echo "请选择重启方式:"
echo "  1) Docker Compose (docker-compose restart backend)"
echo "  2) Systemd (systemctl restart token-monitor-backend)"
echo "  3) 手动重启（稍后自行重启）"
echo ""
read -p "请输入选项 [1-3]: " restart_option

case $restart_option in
    1)
        echo "正在重启 Docker Compose 服务..."
        cd ../..
        docker-compose restart backend
        echo -e "${GREEN}✓ 服务已重启${NC}"
        cd backend/scripts
        ;;
    2)
        echo "正在重启 Systemd 服务..."
        sudo systemctl restart token-monitor-backend
        echo -e "${GREEN}✓ 服务已重启${NC}"
        ;;
    3)
        echo -e "${YELLOW}⚠ 请记得手动重启后端服务以应用连接池配置${NC}"
        ;;
    *)
        echo -e "${RED}无效选项，跳过重启${NC}"
        ;;
esac
echo ""

# 步骤 3: 验证修复
echo -e "${YELLOW}步骤 3/3: 验证修复${NC}"
echo "----------------------------------------"
sleep 2  # 等待服务启动
python verify_performance_fix.py
if [ $? -eq 0 ]; then
    echo ""
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}✓ 性能修复完成！${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""
    echo "修复内容:"
    echo "  ✓ 数据库连接池: 30 → 100"
    echo "  ✓ 关键索引: 新增 10 个"
    echo ""
    echo "预期效果:"
    echo "  • 大屏加载时间: 5秒 → < 1秒"
    echo "  • 并发能力: 30 → 100"
    echo "  • 数据库 CPU: 60% → 20%"
else
    echo ""
    echo -e "${RED}========================================${NC}"
    echo -e "${RED}⚠ 部分修复未完成${NC}"
    echo -e "${RED}========================================${NC}"
    echo ""
    echo "请查看上面的错误信息并手动修复"
fi
