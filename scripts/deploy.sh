#!/bin/bash
# 图床打包脚本：在本机打包源码，上传服务器后执行 scripts/deploy-remote.sh 完成部署。
# 服务器地址可用环境变量覆盖：IMGBED_SERVER / IMGBED_USER / IMGBED_SSH_PORT
set -e

SERVER="${IMGBED_SERVER:-192.168.28.26}"
USER="${IMGBED_USER:-weichen}"
PORT="${IMGBED_SSH_PORT:-22}"

# 始终从仓库根目录打包，与执行时所在目录无关
cd "$(dirname "$0")/.."

echo "📦 打包项目文件..."
# 注意：config.yaml 含本机管理 Token，绝不打入部署包；
# 服务器首次部署时由 deploy-remote.sh 从 config.example.yaml 生成。
tar -czf imgbed-deploy.tar.gz \
    --exclude='./.git' \
    --exclude='./.claude' \
    --exclude='./.codex' \
    --exclude='./.mimocode' \
    --exclude='./out' \
    --exclude='./data' \
    --exclude='./uploads' \
    --exclude='./config.yaml' \
    --exclude='*.exe' \
    --exclude='*.log' \
    --exclude='*.tar.gz' \
    .

echo "✅ 打包完成: imgbed-deploy.tar.gz"
echo ""
echo "接下来手动执行："
echo ""
echo "  1) 上传部署包："
echo "     scp -P ${PORT} imgbed-deploy.tar.gz ${USER}@${SERVER}:~/"
echo ""
echo "  2) 在服务器上执行部署（二选一）："
echo "     # 直接通过 SSH 执行远端部署脚本"
echo "     ssh -p ${PORT} ${USER}@${SERVER} 'bash -s' < scripts/deploy-remote.sh"
echo ""
echo "     # 或登录服务器手动执行"
echo "     ssh -p ${PORT} ${USER}@${SERVER}"
echo "     mkdir -p ~/imgbed && tar -xzf ~/imgbed-deploy.tar.gz -C ~/imgbed"
echo "     cd ~/imgbed && bash scripts/deploy-remote.sh"
