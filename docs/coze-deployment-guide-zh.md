# coze-studio + rag 联调部署指南（开发环境）

**适用范围：** 本地开发环境，`KNOWLEDGE_BACKEND=rag` 模式，含 RAG 服务联调。
**适用分支：** `feat/replace-knowledge-base`（HEAD `0b5250b1` 或更新）

> 仅作为 dev/smoke 用途。生产部署遵循的是 `docker-compose.prod.yml`，不在本文档范围。

---

## 1. 前置条件

- macOS / Linux 主机（其他平台未验证）
- Docker Desktop / Docker Engine 24+（含 Compose v2.24+ 支持 `!override` tag）
- Go **1.24**（不能用 1.26+，详见 §6.1）
- Node 22 + Rush.js（已随仓库 `common/scripts/`）
- Atlas CLI（schema 工具；首次跑 `make sync_db` 时 docker 会拉镜像自动装）
- 本机可用端口：
  - `8888`（coze server）
  - `3306` / `6379` / `9000` / `9001` / `9200` / `27017`（coze middleware）
  - `8000`（rag web）
  - `27018` / `9100` / `9101` / `9201` / `6380`（rag middleware 的 host-side 端口，由 `docker-compose.local.yml` 重映射开避免与 coze 端口冲突）

仓库布局假设：
```
~/workspace/coze-studio/  ← 本仓库
~/workspace/rag/          ← 独立 rag 服务仓库（同级）
```

---

## 2. 一次性配置

### 2.1 rag 端配置

> 这两个文件在 rag 仓库里是 **gitignored**（per-session），首次部署必须创建。

#### `rag/config/model_providers.json`

模型注册表，加载到 rag mongo。从示例复制：

```bash
cd ~/workspace/rag
cp config/model_providers.example.json config/model_providers.json
# 编辑 config/model_providers.json，至少要有：
# - 一个 type=text_embedding 的条目（如 OpenAI text-embedding-3-small）
# - 一个 type=llm 的条目（如 OpenAI gpt-4o-mini，用于 query rewrite）
# - 一个 type=image_embedding 的条目（即便不用，KB 创建要求两个 embedding 都注册）
# 填上真实 api_key
```

最小可用配置示例（编辑后保存）：
```json
{
  "providers": [
    {
      "model_id": "model-openai-text-embed-3-small",
      "name": "openai-embed-3-small",
      "model_name": "text-embedding-3-small",
      "type": "text_embedding",
      "provider": "http",
      "base_url": "https://api.openai.com",
      "endpoint_path": "/v1/embeddings",
      "api_key": "sk-proj-...",
      "dimensions": 1536,
      "capabilities": ["text_embedding"],
      "modalities": ["text"]
    },
    {
      "model_id": "model-openai-gpt-4o-mini",
      "name": "openai-gpt-4o-mini",
      "model_name": "gpt-4o-mini",
      "type": "llm",
      "provider": "http",
      "base_url": "https://api.openai.com",
      "endpoint_path": "/v1/chat/completions",
      "api_key": "sk-proj-...",
      "capabilities": ["llm"]
    },
    {
      "model_id": "model-placeholder-image-embed",
      "name": "placeholder-image-embed",
      "model_name": "unused-placeholder",
      "type": "image_embedding",
      "provider": "http",
      "base_url": "http://image-embed-unused.invalid",
      "endpoint_path": "/v1/image/embeddings",
      "api_key": "",
      "dimensions": 512,
      "capabilities": ["image_embedding"],
      "modalities": ["image"]
    }
  ]
}
```

#### `rag/.env`

```bash
cd ~/workspace/rag
cp .env.example .env
# 确保 MODEL_PROVIDERS_CONFIG_PATH 指向 config/model_providers.json：
sed -i '' 's|^MODEL_PROVIDERS_CONFIG_PATH=.*|MODEL_PROVIDERS_CONFIG_PATH=config/model_providers.json|' .env
```

#### `rag/docker-compose.local.yml`（端口重映射）

把 rag middleware 的 host 端口重映射到非默认值，让 coze middleware 可以继续用标准端口：

```bash
cat > ~/workspace/rag/docker-compose.local.yml <<'EOF'
services:
  mongo:
    ports: !override
      - "27018:27017"
  elasticsearch:
    ports: !override
      - "9201:9200"
  redis:
    ports: !override
      - "6380:6379"
  minio:
    ports: !override
      - "9100:9000"
      - "9101:9001"
EOF
```

⚠️ **`!override` tag 是关键**——普通 `ports:` 列表在 Compose v2 会跟基础文件**累加**，导致冲突仍在。必须用 `!override` 才完全替换。

### 2.2 coze 端配置

#### `docker/.env.debug`

> 该文件 gitignored。从 `.env.example` 复制后编辑。

需要的环境变量（追加到现有内容末尾，或确保已有）：

```bash
# rag backend 开关
export KNOWLEDGE_BACKEND="rag"
export RAG_BASE_URL="http://localhost:8000"

# 租户模式 + 默认租户 ID（必须和 rag config/model_providers.json 的 tenant_id 一致）
export RAG_TENANT_MODE="env"
export RAG_TENANT_ID="coze"

# rag 模型 ID 默认值（必须存在于 model_providers.json）
export RAG_DEFAULT_TEXT_EMBEDDING_MODEL_ID="model-openai-text-embed-3-small"
export RAG_DEFAULT_IMAGE_EMBEDDING_MODEL_ID="model-placeholder-image-embed"
export RAG_DEFAULT_LLM_MODEL_ID="model-openai-gpt-4o-mini"   # R2-F 新增：query rewrite 必需

# rag config 路径（make server 构建后的 cwd 是 bin/，路径相对那个）
export RAG_CONFIG_PATH="resources/conf/rag/rag.yaml"
```

⚠️ **`RAG_DEFAULT_LLM_MODEL_ID` 必须设置**，否则工作流知识检索节点的 query rewrite 会被静默 drop（带 WARN 日志）。基本检索不受影响。

---

## 3. 启动顺序

### 3.1 启动 rag stack

```bash
cd ~/workspace/rag
docker compose -f docker-compose.yml -f docker-compose.local.yml up -d
# 等待 rag 健康
until curl -fs http://localhost:8000/ready >/dev/null 2>&1; do sleep 2; done
echo "rag ready: $(curl -s http://localhost:8000/ready)"
```

期待 `{"checks":{"mongodb":"ok","elasticsearch":"ok","minio":"ok","redis":"ok"}}`。

#### 3.1.1 创建 rag MinIO bucket（首次）

⚠️ **rag 的 `docker-compose.yml` 不会自动建 bucket**。MinIO volume 首次启动后必须手动建一次：

```bash
docker run --rm --network rag_default --entrypoint sh minio/mc:latest -c \
  "mc alias set rag http://minio:9000 minioadmin minioadmin && mc mb rag/rag-files"
```

否则第一次上传文档会 500（`S3Error: NoSuchBucket`）。

### 3.2 启动 coze middleware

```bash
cd ~/workspace/coze-studio
make middleware
```

等到所有 `coze-*` 容器 `Healthy`（约 30-60 秒）。验证：

```bash
docker ps --filter "name=coze-" --format "table {{.Names}}\t{{.Status}}"
```

### 3.3 启动 coze server（原生 Go）

```bash
cd ~/workspace/coze-studio
GOTOOLCHAIN=go1.24.0 make server
```

或者后台启动 + 等就绪：

```bash
GOTOOLCHAIN=go1.24.0 make server > /tmp/coze-server.log 2>&1 &
until lsof -iTCP:8888 -sTCP:LISTEN >/dev/null 2>&1 || grep -qE "panic:|FATAL" /tmp/coze-server.log; do sleep 3; done
echo "coze server: $(lsof -iTCP:8888 -sTCP:LISTEN >/dev/null 2>&1 && echo OK || echo FAIL)"
```

`make server` 构建 + 启动 `./opencoze -start`（cwd = `bin/`）。**注意**：`make server` **不会**自动重建前端。如果前端代码有变更，需要先 `make fe`。

#### 3.3.1 前端代码变更时必须 `make fe`

```bash
cd ~/workspace/coze-studio
make fe       # 重建前端到 bin/resources/static/（约 60-90s）
pkill -f opencoze  # ← 注意：是 opencoze，不是 bin/opencoze
GOTOOLCHAIN=go1.24.0 make server > /tmp/coze-server.log 2>&1 &
```

`make server` 只 copy 既有 `bin/resources/static`；不重建。R2-D-fe-Retry 之后的任何前端改动都必须先 `make fe`。

### 3.4 访问

打开 `http://localhost:8888`，登录现有 mysql 账号（如本地有种子账号 `lxy907360`）或新建账号。

---

## 4. 验证清单

### 4.1 基本 KB + 上传流

1. 进 "数据集" → 新建 → 选 "rag" backend（如果 UI 提供选项）/ 或直接走默认（由 `KNOWLEDGE_BACKEND=rag` 控制）
2. 选 embedding 模型（`<ModelSelector />` 会读 `/api/knowledge_v2/model_providers` 列表）
3. 创建成功后，KB 详情页 → 上传一个小 `.txt` 或 `.pdf`
4. 期待：
   - `POST /api/knowledge/document/create` 返回 200，body 含 `{doc_id, task_id, status: "pending"}`
   - 进度条 10% → 50% → 100%（R2-B 的 progressForStatus）
   - 文档列表显示真实文件名、类型、大小

### 4.2 工作流知识检索（含 query rewrite）

1. 工作流编辑器 → 新建工作流 → 加 "知识库检索" 节点
2. 节点配置：选刚创建的 KB → **勾选 "query rewrite / 查询改写"**
3. 测试运行
4. 期待：节点执行成功（之前 R2-F 之前会报 `rag 40004: query_strategy.llm_model_id is required`）
5. 验证 rag 真实收到带 `llm_model_id` 的请求：
   ```bash
   docker logs rag-web-1 --since=2m | grep -i "POST /api/v1/retrieval"
   # 期待：单次请求耗时 >3s（包含 LLM 调用做 rewrite），HTTP 200
   ```

### 4.3 失败重试（可选）

> 这个验证有点麻烦——rag 的 model config 多层缓存难失效。推荐的注入失败方式是直接 mongo 改 task 状态。

```bash
# 找最新 task
docker exec rag-mongo-1 mongosh ragdb --quiet --eval \
  'db.tasks.findOne({}, {_id:1, status:1}, {sort:{created_at:-1}})'

# 改 status 为 failed
docker exec rag-mongo-1 mongosh ragdb --quiet --eval \
  'db.tasks.updateOne({_id:"<task-uuid>"}, {$set:{status:"failed", error_msg:"synthetic for retry smoke"}})'
```

然后回浏览器 upload 进度页面 → 看到"重试"按钮 → 点 → 验证 `POST /api/knowledge/document/retry` 200，`mapping.last_task_id` 在 mysql 里更新到了新 task ID。

### 4.4 完整性自检

```bash
# coze server 健康
curl -fs http://localhost:8888/api/health 2>&1 || echo "可能需要 session"

# rag 健康
curl -fs http://localhost:8000/ready

# 容器全部 Healthy
docker ps --format "table {{.Names}}\t{{.Status}}" | head -20

# MinIO bucket 存在
docker exec rag-minio-1 mc ls local/rag-files 2>/dev/null || \
  docker run --rm --network rag_default --entrypoint sh minio/mc:latest -c \
    "mc alias set rag http://minio:9000 minioadmin minioadmin && mc ls rag/rag-files"
```

---

## 5. 关停 / 清理

### 完全关停

```bash
# coze server
pkill -f opencoze

# coze middleware（保留数据）
cd ~/workspace/coze-studio && make down

# rag stack（保留数据）
cd ~/workspace/rag && docker compose -f docker-compose.yml -f docker-compose.local.yml down
```

### 清理（删除所有数据，需重新建 bucket / KB）

```bash
cd ~/workspace/coze-studio && make clean    # 销毁 coze 数据卷
cd ~/workspace/rag && docker compose -f docker-compose.yml -f docker-compose.local.yml down -v
```

---

## 6. 常见问题

### 6.1 启动报 sonic 编译错误

```
undefined: GoMapIterator (or similar)
```

**原因**：本地 Go ≥ 1.26 与 `bytedance/sonic v1.14` 不兼容。
**解决**：所有 Go 命令前缀 `GOTOOLCHAIN=go1.24.0`：

```bash
GOTOOLCHAIN=go1.24.0 make server
GOTOOLCHAIN=go1.24.0 go test ./backend/...
```

### 6.2 启动报 `dial tcp 127.0.0.1:9000: connect: connection refused`

coze 的 MinIO（端口 9000）未起来。`make middleware` 没跑完，或容器异常退出。检查：

```bash
docker ps --filter "name=coze-minio" --format "{{.Names}} {{.Status}}"
# 如果不在 Healthy 状态：
make middleware     # 重新拉起
```

### 6.3 上传报 500，rag 日志显示 `NoSuchBucket: rag-files`

rag MinIO bucket 未创建。回到 §3.1.1 跑那条 `mc mb` 命令。

### 6.4 上传成功但停留 "pending"，rag worker 无日志

worker 容器没起来，或 `model_providers.json` 配置错误。检查：

```bash
docker logs rag-worker-1 --since=5m | head -20
docker exec rag-mongo-1 mongosh ragdb --quiet --eval \
  'db.model_providers.find({}, {name:1, type:1, model_name:1, is_active:1, _id:0}).toArray()'
```

确认 `is_active: true` 的 text_embedding 条目存在、`model_name` 是真实 OpenAI 模型名。

### 6.5 工作流检索节点报 `rag 40004: query_strategy.llm_model_id is required`

`RAG_DEFAULT_LLM_MODEL_ID` 未设置或值错。检查：

```bash
grep RAG_DEFAULT_LLM_MODEL_ID ~/workspace/coze-studio/docker/.env.debug
# 应有一行：export RAG_DEFAULT_LLM_MODEL_ID="model-openai-gpt-4o-mini"
# 改了之后必须重启 coze server 让 env 生效
pkill -f opencoze && GOTOOLCHAIN=go1.24.0 make server > /tmp/coze-server.log 2>&1 &
```

也要确认 `model-openai-gpt-4o-mini` 真在 `rag/config/model_providers.json` 里且 `type: "llm"`。

### 6.6 端口冲突

coze middleware 和 rag middleware 都想用 6379/9200/9000/9001/27017。rag 必须用 `docker-compose.local.yml` 的 `!override` ports（见 §2.1）。如果还冲突：

```bash
lsof -iTCP:6379 -sTCP:LISTEN   # 看谁占着
# 通常是宿主机的 redis-server 服务
brew services stop redis        # 或 sudo systemctl stop redis
```

### 6.7 前端改了代码不见效

需要 `make fe`：

```bash
cd ~/workspace/coze-studio
make fe                              # 重建前端
pkill -f opencoze                    # 注意：opencoze 不是 bin/opencoze
GOTOOLCHAIN=go1.24.0 make server > /tmp/coze-server.log 2>&1 &
```

如果 `make fe` 报 "Current PNPM store path does not match the last one used"（仓库目录移动后会出现）：

```bash
cd ~/workspace/coze-studio/frontend && rush update --purge    # 约 80 秒
```

### 6.8 切片管理 / 文档元数据更新 / KB 复制 等操作报 `105100001 feature pending rag support`

预期行为。rag 不暴露 chunk-level API（架构上 chunk 是内部实现），coze 这些 UI 入口在 rag-backed KB 下没意义。

未来 R2-D-fe-Wizard 或 PR-2 会屏蔽这些入口。当前可忽略（不影响 happy path）。

### 6.9 工作流"知识库检索"节点把 rerank 开了报错

预期行为。`EnableRerank` 在 coze 端还没翻译到 rag 的 `query_strategy.enable_rerank`，且 rag 端也需要 `rerank_model_id`。当前实现的范围只到 query rewrite（R2-F），rerank 留给 R2-F-Rerank。

---

## 7. 进度文档 + 设计文档

- 项目整体进度：`docs/rag-replacement-progress-zh.md`
- 每个切片的设计 spec：`docs/superpowers/specs/2026-05-1{2,3,4}-*.md`
- 每个切片的实现 plan：`docs/superpowers/plans/2026-05-1{2,3,4}-*.md`
