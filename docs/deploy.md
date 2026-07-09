# 部署指南

两种部署方式：

- **Docker Compose**：适合任何有 Docker 的服务器，推荐。
- **脚本打包部署**：适合无法直接拉取代码的内网服务器，用 `scripts/deploy.sh` 打包上传。

## 方式一：Docker Compose

### 1. 准备配置

在服务器项目目录执行：

```bash
cp config.example.yaml config.yaml
mkdir -p data uploads
```

编辑 `config.yaml`：

```yaml
server:
  port: 8080
  base_url: "https://你的域名"

auth:
  token: "换成一段足够长的随机字符串"   # 留空则首次启动自动生成

storage:
  backend: local
  local:
    dir: ./uploads
    url_path: /i

database:
  path: ./data/imgbed.db
```

如果先用 IP 测试，`base_url` 可以写成 `http://服务器IP:8080`。

### 2. 启动

```bash
docker compose up -d --build
```

访问 `http://服务器IP:8080`，查看状态：

```bash
docker compose logs -f imgbed
docker compose ps
curl http://127.0.0.1:8080/healthz
```

### 3. 升级

拉取或上传新代码后：

```bash
docker compose up -d --build
```

## 方式二：脚本打包部署（内网服务器）

适用于服务器无法访问代码仓库的场景。目标服务器地址在脚本里有默认值，可用环境变量覆盖。

在本机（仓库根目录）：

```bash
# 打包（自动排除 config.yaml、数据目录、构建产物等）
./scripts/deploy.sh

# 按脚本输出提示上传并部署，例如：
scp imgbed-deploy.tar.gz user@server:~/
ssh user@server 'bash -s' < scripts/deploy-remote.sh
```

`deploy-remote.sh` 在服务器上自动完成：备份旧数据（data/uploads/config.yaml）→ 解压新版本 → 构建镜像 → 启动容器。部署包不含本机 `config.yaml`，不会覆盖服务器上的配置。

自定义目标服务器：

```bash
IMGBED_SERVER=10.0.0.5 IMGBED_USER=root ./scripts/deploy.sh
```

## Nginx 反向代理示例

```nginx
server {
    listen 80;
    server_name img.example.com;

    client_max_body_size 25m;    # 应大于 config 里的 max_size_mb

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

使用反代后，把 `config.yaml` 里的 `server.base_url` 改成外部访问地址（如 `https://img.example.com`），然后：

```bash
docker compose restart imgbed
```

## S3 / CDN 模式

使用 S3 兼容存储（MinIO / 阿里云 OSS / 腾讯云 COS / Cloudflare R2 等）时，把 `config.yaml` 改成：

```yaml
storage:
  backend: s3
  s3:
    endpoint: "s3.amazonaws.com"
    region: "us-east-1"
    bucket: "your-bucket"
    access_key: "your-access-key"
    secret_key: "your-secret-key"
    use_ssl: true
    public_url: "https://cdn.example.com"
```

`public_url` 留空时，图片仍通过本服务的 `/i/...` 中转读取；填 CDN 地址时，复制出的图片链接会直接指向 CDN。

## 备份

需要备份三类文件：

```text
config.yaml
data/
uploads/
```

SQLite 使用 WAL 模式，备份时建议先停容器：

```bash
docker compose stop imgbed
tar czf imgbed-backup-$(date +%F).tar.gz config.yaml data uploads
docker compose start imgbed
```

## 常用命令

```bash
docker ps | grep imgbed          # 查看容器状态
docker logs -f imgbed            # 查看日志
docker compose restart imgbed    # 重启
docker compose down              # 停止并删除容器
docker exec -it imgbed sh        # 进入容器
```

## 常见问题

**端口被占用？** 编辑 `docker-compose.yml` 修改端口映射：

```yaml
ports:
  - "8081:8080"
```

**修改配置后如何生效？** `vim config.yaml` 后 `docker compose restart imgbed`。

**忘记 Token？** 直接查看服务器上的 `config.yaml` 中 `auth.token` 字段。

**上传大文件报 413？** 同时检查 `config.yaml` 的 `limits.max_size_mb` 和反向代理的 `client_max_body_size`。
