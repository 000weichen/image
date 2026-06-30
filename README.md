# IMGHOST 图床

一个轻量级自托管图床，后端使用 Go/Gin，数据存储使用 SQLite，前端为内嵌静态 SPA。支持本地磁盘和 S3 兼容对象存储。

## 功能

- Token 保护的管理后台
- 拖拽、选择文件、剪贴板粘贴上传
- 图片列表、搜索、分页、删除
- 相册创建、编辑、删除、移动图片
- 图片访问量统计
- SHA-256 去重存储
- 图片宽高识别和旧数据回填
- 自定义别名链接，例如 `/s/avatar.png`
- 后台预览不计入访问量
- 本地存储或 S3/CDN 链接生成
- Docker Compose 部署

## 快速开始

```bash
cp config.example.yaml config.yaml
mkdir -p data uploads
```

编辑 `config.yaml`：

```yaml
server:
  port: 8080
  base_url: "http://localhost:8080"

auth:
  token: "change-me-to-a-long-random-token"
```

本地运行：

```bash
go run .
```

访问：

```text
http://localhost:8080
```

## Docker 部署

```bash
docker compose up -d --build
```

更多服务器部署、Nginx/Lucky 反代、备份和 S3 配置见 [DOCKER_DEPLOY.md](DOCKER_DEPLOY.md)。

## 配置说明

`config.yaml` 不应提交到 Git，因为里面包含管理 Token 和部署环境配置。请从 `config.example.yaml` 复制生成。

关键配置：

- `server.base_url`: 生成公开图片链接的外部访问地址，例如 `https://image.example.com`
- `auth.token`: 管理后台和 API Token
- `storage.backend`: `local` 或 `s3`
- `storage.local.dir`: 本地图片目录
- `storage.s3.public_url`: CDN 或公开对象存储地址
- `database.path`: SQLite 数据库路径

## 公开链接格式

默认 hash 链接：

```text
/i/bb/bbffe223e19690701d3330dbaf2c94ecd38b866cbb38041855e5eb162cb614b5.png
```

自定义别名链接：

```text
/s/avatar.png
```

别名只改变分享链接，不改变底层文件名。原 hash 链接会继续可用。

## 目录

```text
internal/          Go 后端代码
web/               前端页面、样式和脚本
config.example.yaml 配置示例
Dockerfile         容器构建文件
docker-compose.yml Compose 部署文件
```

## 备份

需要备份：

```text
config.yaml
data/
uploads/
```

建议备份前先暂停容器：

```bash
docker compose stop imgbed
tar czf imgbed-backup-$(date +%F).tar.gz config.yaml data uploads
docker compose start imgbed
```
