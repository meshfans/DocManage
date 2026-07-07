#!/bin/bash

# Doc Server 构建脚本
# 编译并输出到 bin 目录

echo "========================================"
echo "  Doc Server 构建脚本"
echo "========================================"
echo ""

# 获取脚本所在目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR" || exit 1

# 创建输出目录
mkdir -p bin

echo "开始构建..."

# Step 1: 打包前端
echo "[1/4] 打包前端..."
FRONT_DIR="$(dirname "$SCRIPT_DIR")/frontend"
cd "$FRONT_DIR" || exit 1
pnpm build
if [ $? -ne 0 ]; then
    echo "❌ 前端打包失败"
    exit 1
fi
echo "✅ 前端打包成功"
cd "$SCRIPT_DIR" || exit 1

# Step 2: 清理旧的 dist
echo "[2/4] 清理旧的前端文件..."
WEB_DIST_DIR="$SCRIPT_DIR/web/dist"
if [ -d "$WEB_DIST_DIR" ]; then
    rm -rf "$WEB_DIST_DIR"
    echo "✅ 旧 dist 已删除"
else
    echo "✅ 无旧 dist，跳过清理"
fi

# Step 3: 复制前端文件到 web 目录
echo "[3/4] 复制前端文件到 web 目录..."
FRONT_DIST_DIR="$(dirname "$SCRIPT_DIR")/frontend/dist"
if [ ! -d "$FRONT_DIST_DIR" ]; then
    echo "❌ 前端 dist 目录不存在: $FRONT_DIST_DIR"
    exit 1
fi

mkdir -p "$WEB_DIST_DIR"
cp -r "$FRONT_DIST_DIR/"* "$WEB_DIST_DIR/"
echo "✅ 前端文件已复制"

# Step 4: 编译后端
echo "[4/4] 编译后端..."
go build -ldflags "-s -w" -o bin/doc-server main.go
if [ $? -eq 0 ]; then
    echo "✅ 后端编译成功"
else
    echo "❌ 后端编译失败"
    exit 1
fi

echo ""
echo "========================================"
echo "  构建完成"
echo "========================================"
echo ""
echo "输出目录: bin"
echo "可执行文件:"
ls -lh bin/*.exe bin/doc-server 2>/dev/null || ls -lh bin/
echo ""
