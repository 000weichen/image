# IMGHOST 图床

轻量级自托管图床。后端 Go + Gin + SQLite，前端为嵌入二进制的原生 JS 单页应用——编译产物是**一个可执行文件**，无外部依赖。支持本地磁盘与 S3 兼容对象存储。

## 特性

**上传与存储**

- 拖拽、文件选择、剪贴板粘贴三种上传方式，带进度显示
- SHA-256 内容去重：相同图片只存一份，重复上传直接返回已有记录
- 类型白名单 + 内容嗅探双重校验（默认 JPEG / PNG / GIF / WebP / BMP，可配置）
- 自动识别图片宽高，旧数据启动时自动回填
- 本地磁盘或 S3 兼容存储（MinIO / 阿里云 OSS / 腾讯云 COS / Cloudflare R2 等），支持 CDN 直链

**管理后台**（Token 保护）

- 仪表盘：总量统计、最近 7 天上传趋势图、热门图片 Top 10
- 图片管理：搜索（文件名 / Hash / 别名）、分页、相册筛选、一键复制 URL / Markdown
- 相册：创建、编辑、删除、图片归类
- 自定义别名短链：`/s/avatar.png`，别名与原 hash 链接同时可用
- 浅色 / 深色双主题，跟随本地存储记忆

**工程细节**

- 首次运行自动生成 `config.yaml` 并打印随机管理 Token，零配置即可启动
- 访问计数在内存聚合、批量落库，公开图片请求路径上无数据库写操作
- 图片响应带一年强缓存头；后台预览走独立路由，不计入访问量
- SQLite WAL 模式 + 优雅关闭（退出前落盘未写入的计数）
- 安全响应头（CSP、X-Frame-Options、nosniff）、路径穿越防护、常数时间 Token 比较

## 快速开始

### 本地运行

```bash
go run .
```

首次运行会自动生成 `config.yaml`，并在控制台打印随机管理 Token：

```text
已生成默认配置 config.yaml，管理员 Token: 7380d20c0d696782...
图床服务已启动: http://localhost:8080
```

浏览器打开 `http://localhost:8080`，粘贴 Token 进入管理后台。

### 编译

```bash
# Windows
go build -o imgbed.exe .

# Linux（静态编译，可直接扔服务器）
CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o imgbed .
```

### Docker

```bash
docker compose up -d --build
```

服务器部署、Nginx 反代、S3 配置、备份等见 [docs/deploy.md](docs/deploy.md)。

## 配置说明

配置文件默认读取工作目录的 `config.yaml`，可用环境变量 `IMGBED_CONFIG` 指定其他路径。完整模板见 [config.example.yaml](config.example.yaml)。

| 配置项 | 默认值 | 说明 |
|---|---|---|
| `server.port` | `8080` | 监听端口 |
| `server.base_url` | `http://localhost:8080` | 生成对外图片链接的前缀，反代后改为外部域名 |
| `auth.token` | 自动生成 | 管理后台与 API 的 Token；留空则首次启动生成随机值并写回 |
| `storage.backend` | `local` | `local` 或 `s3` |
| `storage.local.dir` | `./uploads` | 本地存储目录 |
| `storage.local.url_path` | `/i` | 公开取图路由前缀 |
| `storage.s3.*` | — | S3 端点、桶、密钥等，见示例文件注释 |
| `storage.s3.public_url` | 空 | 留空走服务端中转；填 CDN 地址则链接直接指向 CDN |
| `database.path` | `./data/imgbed.db` | SQLite 数据库文件 |
| `limits.max_size_mb` | `20` | 单文件大小上限 |
| `limits.allowed_types` | 五种常见格式 | 允许的 MIME 类型白名单 |

> `config.yaml` 包含管理 Token，已被 `.gitignore` 排除，不要提交到仓库。

## API

管理 API 需要 Token，两种传递方式任选：

```text
Authorization: Bearer <token>
X-API-Token: <token>
```

| 方法 | 路径 | 说明 |
|---|---|---|
| `POST` | `/api/upload` | multipart 上传，字段 `file`；可选 `album`（相册 id 或名称） |
| `GET` | `/api/images?page=&album=&q=` | 分页列表；`album=0` 为未分类；`q` 搜文件名 / Hash / 别名 |
| `GET` | `/api/images/:id` | 图片详情 |
| `PATCH` | `/api/images/:id` | 移动相册：`{"album":"2"}`，空串或 `"0"` 表示移出 |
| `PATCH` | `/api/images/:id/alias` | 设置别名：`{"alias":"my.png"}`，空串清除 |
| `DELETE` | `/api/images/:id` | 删除图片（含物理文件） |
| `GET` | `/api/albums` | 相册列表（含图片数） |
| `POST` | `/api/albums` | 创建：`{"name":"...","description":"..."}` |
| `PUT` | `/api/albums/:id` | 修改名称 / 描述 |
| `DELETE` | `/api/albums/:id` | 删除相册（图片自动变为未分类） |
| `GET` | `/api/stats` | 全局统计、每日趋势、热门图片 |
| `GET` | `/api/config` | 上传限制与存储后端信息 |

公开路由（无需 Token）：

| 路径 | 说明 |
|---|---|
| `/` | 管理后台页面 |
| `/i/<hash 路径>` | 图片访问，计入访问量 |
| `/s/<别名>` | 别名访问，计入访问量 |
| `/preview/<hash 路径>` | 后台预览，不计访问量 |
| `/healthz` | 健康检查 |

命令行上传示例（可用于接入 PicGo 等工具的自定义上传器）：

```bash
curl -H "X-API-Token: <token>" -F "file=@photo.png" http://localhost:8080/api/upload
# 返回 JSON 中的 url 字段即公开链接
```

## 公开链接格式

```text
默认 hash 链接：/i/bb/bbffe223e19690701d…14b5.png
自定义别名链接：/s/avatar.png
```

别名只是新增一个入口，底层文件名不变，原 hash 链接始终可用。

## 项目结构

```text
├── main.go                  # 入口：路由注册、前端资源嵌入、优雅关闭
├── internal/
│   ├── auth/                # Token 鉴权中间件
│   ├── config/              # 配置加载、默认值与首次运行初始化
│   ├── db/                  # SQLite 初始化、建表与迁移
│   ├── handler/             # HTTP 处理器（图片、相册、上传、统计）
│   ├── model/               # 数据模型与 API 响应结构
│   ├── service/             # 业务逻辑：上传去重、访问计数聚合、统计
│   └── storage/             # 存储抽象：本地磁盘 / S3 两种实现
├── web/                     # 前端 SPA（index.html + app.js + style.css，嵌入二进制）
├── docs/
│   └── deploy.md            # 部署指南（Docker / 脚本 / Nginx / S3 / 备份）
├── scripts/
│   ├── deploy.sh            # 本机打包脚本
│   └── deploy-remote.sh     # 服务器端部署脚本
├── config.example.yaml      # 配置模板
├── Dockerfile               # 多阶段构建，产出非 root 运行的最小镜像
├── docker-compose.yml
└── docker-entrypoint.sh
```

## 备份

只需备份三样东西：

```text
config.yaml   # 配置与 Token
data/         # SQLite 数据库
uploads/      # 图片文件（本地存储模式）
```

SQLite 使用 WAL 模式，备份前建议先停服务，完整命令见 [docs/deploy.md](docs/deploy.md#备份)。

## 安全说明

- 管理 API 全部经 Token 鉴权；图片访问路由是公开的——知道链接的任何人都能查看图片，请勿上传敏感内容
- Token 泄露后直接修改 `config.yaml` 中的 `auth.token` 并重启即可作废旧值
- 上传经内容嗅探校验，白名单不含 SVG 等可执行内容的格式；文件名由内容 hash 生成，用户输入不进入存储路径
