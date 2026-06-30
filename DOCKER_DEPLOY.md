# Docker 部署

## 1. 准备配置

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
  token: "换成一段足够长的随机字符串"

storage:
  backend: local
  local:
    dir: ./uploads
    url_path: /i

database:
  path: ./data/imgbed.db
```

如果先用 IP 测试，`base_url` 可以写成 `http://服务器IP:8080`。

## 2. 启动

```bash
docker compose up -d --build
```

访问：

```text
http://服务器IP:8080
```

查看日志：

```bash
docker compose logs -f imgbed
```

查看健康状态：

```bash
docker compose ps
curl http://127.0.0.1:8080/healthz
```

## 3. Nginx 反向代理示例

```nginx
server {
    listen 80;
    server_name img.example.com;

    client_max_body_size 25m;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

使用反代后，把 `config.yaml` 里的 `server.base_url` 改成你的外部访问地址，例如：

```yaml
server:
  base_url: "https://img.example.com"
```

然后重启：

```bash
docker compose restart imgbed
```

## 4. 升级

拉取或上传新代码后：

```bash
docker compose up -d --build
```

## 5. 备份

需要备份这三类文件：

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

## 6. S3/CDN 模式

如果使用 S3 兼容存储，把 `config.yaml` 改成：

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

`public_url` 留空时，图片仍会通过本服务的 `/i/...` 中转读取；填 CDN 地址时，复制出的图片链接会直接指向 CDN。
