# Coze Studio 本地前后端启动流程

本文档整理当前仓库已有的启动方式，适用于本地体验、前端源码修改验证、后端源码调试和 Docker 镜像构建验证。

## 目录

- **方式五（推荐）**：全栈本地开发，前后端源码改动即时可见。
- `make web`：完整 Docker 启动，适合快速体验。
- `make debug`：本地后端调试启动，依赖中间件使用 Docker。
- `rushx dev`：本地前端开发启动，适合验证前端源码修改。

## 前置要求

- 已安装并启动 Docker / Docker Compose。
- 前端开发需要 Node.js >= 21、Rush、PNPM。
- 后端开发需要 Go 环境，并能访问 Go module proxy。

## 方式五：全栈本地开发（推荐）

同时修改 `frontend/` 与 `backend/` 时，使用「中间件 Docker + 本地 Go + Rsbuild dev」组合。**不要**用 `http://localhost:8890/` 验证源码改动（那是 Docker 预构建页面）。

| 地址 | 用途 |
| --- | --- |
| `http://localhost:8090/` | 前端 dev（热更新），日常开发入口（默认端口，见下） |
| `http://localhost:8888/` | 本地 Go API；注册账号 `http://localhost:8888/sign` |
| `http://localhost:8890/` | 仅 `make web` 的 Docker 页面，不含本地源码 |

快速查看启动步骤：

```bash
cd /home/z/zhaoxi/coze-studio
make dev-hint
```

### 一次性准备

```bash
cd /home/z/zhaoxi/coze-studio
cp docker/.env.debug.example docker/.env.debug   # 若不存在
make middleware

cd frontend && rush install
```

首次使用需在 `backend/conf/model/` 配置模型，并在 `http://localhost:8888/sign` 注册账号。

### 日常两个终端

**终端 A — 本地 Go 后端**（改 `backend/` 后需 `Ctrl+C` 再执行 `make server`）：

```bash
cd /home/z/zhaoxi/coze-studio
make server
```

**终端 B — 前端 dev**（改 `frontend/` 自动热更新）：

```bash
cd /home/z/zhaoxi/coze-studio/frontend/apps/coze-studio
rushx dev
```

浏览器打开终端里 Rsbuild 打印的地址（默认 **`http://localhost:8090/`**）。

### 前端 dev 端口

默认使用 **8090**（避免与占用 **8080** 的其它服务冲突，例如 cAdvisor）。可覆盖：

```bash
COZE_DEV_SERVER_PORT=8091 rushx dev
```

`strictPort` 已关闭：若指定端口仍被占用，Rsbuild 会自动尝试下一个可用端口。

### 试运行上传失败（Request failed: NETERROR）

工作流节点「上传」依赖后端返回的 `upload_host` 与当前页面 **同源**。本地开发请同时满足：

1. **终端 1** 已执行 `make middleware && make server`（`8888` 有 Go 服务）。
2. **终端 2** 用 `rushx dev` 打开页面（默认 `http://127.0.0.1:8090`），不要用 `8890` 的 Docker 页面混测上传。
3. 在 **同一地址** 登录（例如始终在 `http://127.0.0.1:8090` 登录后再试运行；不要 `localhost` 与 `127.0.0.1` 混用）。
4. 前端 dev 代理已设置 `changeOrigin: false`，避免上传 URL 被改写成 `8888` 导致跨域失败。

若仍失败，在浏览器 Network 里查看 `GetUploadAuthToken` 与 `apply_upload_action` 是否 200。

### 如何确认「本地源码」已生效

按下面顺序自检；任一步不符合，说明页面或 API **没有**走到你刚改的 `backend/` 二进制。

| 检查项 | 本地开发（正确） | 未走本地后端（常见误判） |
| --- | --- | --- |
| 浏览器地址 | `http://127.0.0.1:8090/`（`rushx dev`） | `http://localhost:8890/`（`make web` 预构建页） |
| 宿主机 `8888` | `ss -tlnp \| grep 8888` 有 `opencoze` 进程 | 无监听，或只有 `docker-proxy` / `coze-server` 容器 |
| 改 Go 后 | 必须 `Ctrl+C` 后重新 `make server`（会重新 `go build`） | 只保存文件、只开 `rushx dev` |
| OCR/上传 URL | 多为 `.../local_storage/opencoze/...` 或 `127.0.0.1:9000` | 工作流里仍是 `http://minio:9000/...` 且未重建后端 → 易出现 SSRF |

**1. 确认 API 代理目标**

前端 dev 默认把 `/api` 代理到 `http://127.0.0.1:8888/`。在终端执行：

```bash
ss -tlnp | grep -E '8888|8090'
```

应同时看到 **8090**（rsbuild）和 **8888**（`bin/opencoze`）。若只有 8090，工作流请求会失败或打到别的环境。

**2. 确认本地 Go 已重新编译**

```bash
cd /home/z/zhaoxi/coze-studio
make server
```

日志里应有 `Build completed successfully!` 与 `Starting Go service...`。

**3. 用接口探活（可选）**

```bash
curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8888/api/playground/health 2>/dev/null || curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:8888/health
```

返回 `200` 表示当前页面代理到的后端在本机 `8888` 可用。

**4. 看 OCR 报错是否变化**

- 旧逻辑（HTTP 拉 `minio:9000`）：`access to private/internal network address is not allowed`
- 新逻辑未起后端：`platform storage client is not initialized`
- 新逻辑已起后端、MinIO 正常：应进入 OCR 调用，不再出现上述 SSRF 文案
- `No PDF found in messages`：OCR 节点把 PNG 转成单页 PDF 后发给下游；若下游是 **仅 PDF** 的 OpenAI 兼容服务（如 DeepSeek-OCR 包装），Endpoint 填服务根地址（如 `http://127.0.0.1:17000`），不要用 `/v1/chat/completions`。若仍报错，在 OCR 配置 JSON 中加 `"message_format": "pdf_data_url_string"`。若用 **EasyOCR**（支持图片+PDF），同样只填根地址即可。
- **PaddleOCR Docker（推荐）**：Request format 选 **PaddleOCR (base64 data URL)**。Coze 从 MinIO 读文件后生成 `data:application/pdf;base64,...`（图片会先转成单页 PDF），`POST {endpoint}/v1/chat/completions`，body 与本地脚本一致：`messages[0].content` 为 data URL 字符串，`paddleocr.output_format=text`，`stream=false`。Endpoint 用 **PaddleOCR 端口**（如 `http://127.0.0.1:17007`），Model 填 `paddleocr-pdf`。**不要**填 MinerU 的 `:17006`，否则会出现 `hybrid-auto-engine` / CUDA 804。
- **EasyOCR**：Request format 选 **Image (Docker / EasyOCR)**，Endpoint 如 `http://host:17005`，Model `easyocr-ocr`。
- **MinerU**：仅当镜像暴露 `/file_parse` 时使用 Provider **MinerU**；不要用 OpenAI Vision 指到 MinerU 端口。

**5. 前端热更新范围**

- 改 `frontend/`：保存后 `8090` 页面自动刷新即可。
- 改 `backend/`：**不会**热更新，必须重启 `make server`。

若必须用 `make web`（8890）验证后端改动，需 **重新构建** `coze-server` 镜像，而不是只改宿主机上的 Go 源码：

```bash
make build_docker   # 或项目文档中的 server 镜像构建目标
make web
```

### `make middleware` 报 `coze-elasticsearch is unhealthy`

`make middleware` 使用 `docker/docker-compose-debug.yml`。Elasticsearch 需要安装 smartcn 插件、启动 JVM，并在后台执行 `setup_es.sh` 后才会创建 `/tmp/es_init_complete`，首次启动在 WSL2 上常需 **1–3 分钟**。

若 `docker compose --wait` 在约 60 秒内结束并报 unhealthy，多半是健康检查 `start_period` 过短，而不是 ES 真的起不来。可先查看：

```bash
docker logs coze-elasticsearch --tail 80
docker inspect coze-elasticsearch --format '{{.State.Health.Status}}'
```

日志里出现 `Elasticsearch is ready!` 且 `Elasticsearch initialization completed successfully!` 后，容器通常会变为 healthy。此时可直接：

```bash
make server
```

或按新配置重建 ES 后再起中间件：

```bash
docker compose -f docker/docker-compose-debug.yml --env-file docker/.env.debug --profile middleware up -d elasticsearch --force-recreate --wait
make middleware
```

### 重要约束

- **不要**与 `make web` 同时跑：`make web` 会把 Docker `coze-server` 占满宿主机 `8888`，与本地 `bin/opencoze` 冲突。
- 全栈开发用 `make middleware`（只起中间件），不要用 `make web` 起完整栈后再 `make server`。
- 工作流节点里若保存的是 `http://minio:9000/opencoze/...`，本地 `make server` 会通过 MinIO SDK（`127.0.0.1:9000`）按 object key 读取，**不要**对该 URL 做 HTTP 下载。

### 前端 API 代理

`frontend/apps/coze-studio/rsbuild.config.ts` 默认将 `/api`、`/v1`–`/v3`、`/admin`、`/open_api` 代理到 `http://127.0.0.1:8888/`。

通过环境变量覆盖（例如走 Docker nginx 的 MinIO URL 重写）：

```bash
COZE_API_PROXY_TARGET=http://127.0.0.1:8890/ rushx dev
```

连接远程环境时同样设置 `COZE_API_PROXY_TARGET` 为远程地址即可。

## 方式一：完整 Docker 启动

这是仓库 README 推荐的快速启动方式。它会启动 MySQL、Redis、Elasticsearch、MinIO、Milvus、NSQ、Coze Server 和 Coze Web。

```bash
cd /home/z/zhaoxi/coze-studio
make web
```

等价于：

```bash
cd /home/z/zhaoxi/coze-studio
cp ./docker/.env.example ./docker/.env  # 首次启动时 make web 会自动创建
docker compose -f docker/docker-compose.yml --env-file ./docker/.env up -d
```

默认端口：

- `coze-server`：宿主机 `8888` -> 容器 `8888`
- `coze-web`：宿主机 `${WEB_LISTEN_ADDR:-8890}` -> 容器 `80`

浏览器访问：

```text
http://localhost:8890/
```

停止：

```bash
cd /home/z/zhaoxi/coze-studio
make down_web
```

或：

```bash
docker compose -f docker/docker-compose.yml --env-file ./docker/.env down
```

### 注意：Docker 启动不一定包含本地前端修改

当前 `docker/docker-compose.yml` 中 `coze-web` 默认使用预构建镜像：

```yaml
image: cozedev/coze-studio-web:latest
```

因此仅执行 `docker compose up -d` 不会自动把本地 `frontend/` 代码打进页面。如果要在 Docker Web 镜像里看到本地前端修改，需要重新构建 `coze-web` 镜像，见“方式四”。

## 方式二：本地前端开发启动

这是验证前端源码修改的推荐方式，例如修改了：

```text
frontend/packages/workflow/playground/...
frontend/apps/coze-studio/...
```

先启动后端和依赖（仅改前端、后端用 Docker 时可用 `make web`；**前后端同时改请用方式五**）：

```bash
cd /home/z/zhaoxi/coze-studio
make web
```

安装前端依赖：

```bash
cd /home/z/zhaoxi/coze-studio/frontend
rush install
```

启动前端开发服务：

```bash
cd /home/z/zhaoxi/coze-studio/frontend/apps/coze-studio
rushx dev
```

打开 **`http://localhost:8090/`**（或终端输出的实际端口）。不要用 `http://localhost:8890/` 验证本地前端源码改动，因为 `8890` 来自 Docker `coze-web` 镜像。

### 前端 API 代理

配置见 `frontend/apps/coze-studio/rsbuild.config.ts`。默认 `COZE_API_PROXY_TARGET` 为 `http://127.0.0.1:8888/`（本地 Go）。连接 Docker nginx 网关时可设为 `http://127.0.0.1:8890/`。详见方式五。

## 方式三：本地后端调试启动

`make debug` 会先启动中间件 Docker 环境，再构建并运行本地 Go 后端。

```bash
cd /home/z/zhaoxi/coze-studio
make debug
```

它的 Makefile 依赖链为：

```text
make debug
  -> make env
  -> make middleware
  -> make python
  -> make server
```

相关文件：

- 环境文件：`docker/.env.debug`
- Docker Compose：`docker/docker-compose-debug.yml`
- 后端构建脚本：`scripts/setup/server.sh`
- 前端静态目录：`backend/static`、`bin/resources/static`

如果 `bin/resources/static` 不存在，`make server` 会先执行 `make fe` 构建前端静态资源。

停止 debug 中间件：

```bash
cd /home/z/zhaoxi/coze-studio
make down
```

## 方式四：重新构建 Docker Web 镜像

如果希望“网页端 Docker 服务”直接包含本地前端修改，需要让 `coze-web` 从本地源码构建，而不是拉取 `cozedev/coze-studio-web:latest`。

将 `docker/docker-compose.yml` 中 `coze-web` 的 build 配置打开，例如：

```yaml
coze-web:
  build:
    context: ..
    dockerfile: frontend/Dockerfile
  image: cozedev/coze-studio-web:local
```

然后构建并启动：

```bash
cd /home/z/zhaoxi/coze-studio
docker compose -f docker/docker-compose.yml --env-file ./docker/.env up -d --build coze-web
```

访问：

```text
http://localhost:8890/
```

这种方式构建时间较长。日常开发更推荐使用“方式二”的 `rushx dev`。

## 常见问题

### 容器名已存在

报错示例：

```text
Conflict. The container name "/coze-redis" is already in use
```

先确认容器来源：

```bash
docker ps -a --filter name=coze-redis
```

如果确认是当前 Coze Studio 的旧容器，可以清理后重启：

```bash
cd /home/z/zhaoxi/coze-studio
docker compose -f docker/docker-compose.yml --env-file ./docker/.env down
docker rm -f coze-redis
make web
```

### 端口 8888 已被占用

报错示例：

```text
Bind for 0.0.0.0:8888 failed: port is already allocated
```

查看占用者：

```bash
docker ps --format "table {{.ID}}\t{{.Names}}\t{{.Ports}}" | grep 8888
sudo ss -ltnp | grep ':8888'
```

`coze-server` 默认会绑定宿主机 `8888`。不要让 `coze-web` 也绑定 `8888`。建议在 `docker/.env` 中保持：

```env
WEB_LISTEN_ADDR=8890
```

然后重启：

```bash
cd /home/z/zhaoxi/coze-studio
docker compose -f docker/docker-compose.yml --env-file ./docker/.env down
docker compose -f docker/docker-compose.yml --env-file ./docker/.env up -d
```

### 修改前端后页面没有变化

先确认你打开的是哪一个页面：

- `http://localhost:8890/`：Docker `coze-web` 页面，默认来自镜像。
- Rsbuild 输出的 dev server 地址：本地前端源码页面，会热更新。

如果你只是执行了 `make web` 或 `docker compose up -d`，本地前端源码修改不会自动进入 Docker Web 页面。请使用 `rushx dev`，或重新构建 `coze-web` 镜像。

## 推荐工作流

日常全栈开发（前后端同时改）：

```bash
cd /home/z/zhaoxi/coze-studio
make dev-hint   # 查看双终端命令

# 终端 1
make middleware && make server

# 终端 2
cd frontend/apps/coze-studio && rushx dev
# 打开 http://localhost:8090
```

仅改前端、后端用 Docker：

```bash
cd /home/z/zhaoxi/coze-studio
make web
cd frontend && rush install
cd apps/coze-studio && rushx dev
```

仅改后端：

```bash
cd /home/z/zhaoxi/coze-studio
make debug
```

完整 Docker 验证：

```bash
cd /home/z/zhaoxi/coze-studio
make down_web
make web
```

前端源码已修改且必须用 Docker Web 验证时：

```bash
cd /home/z/zhaoxi/coze-studio
docker compose -f docker/docker-compose.yml --env-file ./docker/.env up -d --build coze-web
```
