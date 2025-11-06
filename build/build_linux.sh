#!/bin/bash

# 设置错误时退出
set -e

# 输出颜色配置
GREEN="\033[0;32m"
YELLOW="\033[1;33m"
RED="\033[0;31m"
NC="\033[0m" # No Color

echo -e "${GREEN}=== DedupFS Linux 编译脚本 ===${NC}"

# 获取脚本所在目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

# 进入Linux平台源码目录
cd "$ROOT_DIR/platform/dedupfs_linux"

echo -e "${YELLOW}当前工作目录: $(pwd)${NC}"

# 检查Go是否安装
if ! command -v go &> /dev/null; then
    echo -e "${RED}错误: 未找到Go编译器，请先安装Go${NC}"
    exit 1
fi

echo -e "${GREEN}Go版本: $(go version)${NC}"

# 设置Go环境变量
export GO111MODULE=on
# 设置CGO_ENABLED为1以支持可能的C依赖
export CGO_ENABLED=0
# 设置目标平台为Linux
export GOOS=linux
export GOARCH=amd64

echo -e "${YELLOW}正在安装依赖...${NC}"
go mod tidy
go mod download

echo -e "${YELLOW}正在编译...${NC}"
# 编译并输出到根目录
go build -o "$ROOT_DIR/dedupfs"

if [ $? -eq 0 ]; then
    echo -e "${GREEN}✅ 编译成功!${NC}"
    echo -e "${GREEN}可执行文件路径: $ROOT_DIR/dedupfs${NC}"
    
    # 检查文件大小
    FILE_SIZE=$(du -h "$ROOT_DIR/dedupfs" | cut -f1)
    echo -e "${YELLOW}文件大小: $FILE_SIZE${NC}"
    
    # 设置执行权限
    chmod +x "$ROOT_DIR/dedupfs"
    echo -e "${GREEN}已设置执行权限${NC}"
    
    echo -e "${GREEN}\n编译完成!${NC}"
else
    echo -e "${RED}❌ 编译失败!${NC}"
    exit 1
fi