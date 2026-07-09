#!/bin/bash
# 在服务器上执行的部署脚本：备份旧数据 → 解压新版本 → 构建镜像 → 启动容器。
# 假设部署包已上传到 ~/imgbed-deploy.tar.gz，可用环境变量覆盖：
#   IMGBED_DIR     部署目录（默认 ~/imgbed）
#   IMGBED_TARBALL 部署包路径（默认 ~/imgbed-deploy.tar.gz）
set -e

DEPLOY_DIR="${IMGBED_DIR:-$HOME/imgbed}"
TARBALL="${IMGBED_TARBALL:-$HOME/imgbed-deploy.tar.gz}"

# 兼容新旧两种 Compose 命令
compose() {
    if docker compose version >/dev/null 2>&1; then
        docker compose "$@"
    else
        docker-compose "$@"
    fi
}

echo "🚀 开始部署图床新版本..."

if [ ! -f "$TARBALL" ]; then
    echo "❌ 未找到部署包: $TARBALL" >&2
    echo "   请先在本机运行 scripts/deploy.sh 打包并上传。" >&2
    exit 1
fi

# 停止并删除旧容器
echo "🛑 停止旧容器..."
docker stop imgbed 2>/dev/null || true
docker rm imgbed 2>/dev/null || true

mkdir -p "$DEPLOY_DIR"
cd "$DEPLOY_DIR"

# 备份旧数据
if [ -d "data" ] || [ -d "uploads" ] || [ -f "config.yaml" ]; then
    BACKUP_NAME="backup-$(date +%Y%m%d-%H%M%S).tar.gz"
    echo "📦 备份旧数据: $BACKUP_NAME"
    tar -czf "$BACKUP_NAME" data uploads config.yaml 2>/dev/null || true
fi

# 解压新版本（包内不含 config.yaml，不会覆盖服务器配置）
echo "📦 解压新版本..."
tar -xzf "$TARBALL"

# 检查配置文件
if [ ! -f "config.yaml" ]; then
    echo "⚠️  未找到 config.yaml，从示例复制..."
    cp config.example.yaml config.yaml
    echo "⚠️  首次启动会自动生成随机 Token，可用 docker logs imgbed 查看，"
    echo "    或直接编辑 config.yaml 设置：vim config.yaml"
fi

# 构建并启动
echo "🔨 构建 Docker 镜像..."
docker build -t imgbed:latest .

echo "🚀 启动新容器..."
compose up -d

sleep 3

echo ""
echo "✅ 部署完成！"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
docker ps | grep imgbed || true
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "🌐 访问地址: http://$(hostname -I 2>/dev/null | awk '{print $1}'):8080"
echo "📊 查看日志: docker logs -f imgbed"
echo "🔧 进入容器: docker exec -it imgbed sh"
