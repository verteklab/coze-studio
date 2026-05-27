# Coze Studio 部署指南（Docker，含并行实例）

**适用范围：** 通过 Docker Compose 部署 coze-studio（`KNOWLEDGE_BACKEND=rag`），覆盖两种场景：
1. **全新单实例**：服务器上还没有 coze
2. **并行部署**：服务器上已经有一套 coze 在跑，需要再起一套互不干扰

**分支：** `feat/replace-knowledge-base`

> 不依赖 `make server` 原生构建路径；那条流程见 §附录 A。

---

## 1. 部署形态总览

```
┌─ coze-studio-v2 (project: coze-studio-v2) ─────────┐
│  coze-server-v2  :<API_PORT>    (容器内 :8888)     │
│  coze-web-v2     :<WEB_PORT>    (nginx 反代)       │
│  coze-mysql-v2   :<DB_PORT>     (Atlas auto-init)  │
│  coze-{redis,es,minio,milvus,etcd,nsq*}-v2          │
│  数据卷: <DATA_DIR>/{mysql,minio,milvus,bitnami/*}  │
│  network: coze-studio-v2_coze-network               │
│                              + rag_default (外部加入) ─┐
└─────────────────────────────────────────────────────┘  │
                                                         ▼
                                      ┌─ 已有 RAG ────────┐
                                      │ rag-web :8000     │
                                      │ Mongo / ES / MinIO│
                                      └───────────────────┘
```

并行部署的关键隔离手段（缺一不可）：

| 维度 | 隔离方式 |
|---|---|
| Compose project | `-p coze-studio-v2` |
| 容器名 | `coze-*-v2`（在 v2.yml 里逐个 `container_name:` 覆盖） |
| 镜像 tag | `coze-studio-{server,web}-v2:latest`（独立 build） |
| 宿主端口 | `<WEB_PORT> / <API_PORT> / <DB_PORT>`（用 `!override` 替换） |
| 数据目录 | `<DATA_DIR>`（不是默认的 `./data`） |
| RAG tenant | `RAG_TENANT_ID` 不能跟旧实例同名 |

---

## 2. 前置条件

- Linux 主机 + Docker Engine 24+ / Compose v2.24+（必须支持 `!override` tag）
- 一个可用的 **RAG 服务**——可以是：
  - 同台机器上已经有 `rag-web` 容器（推荐复用，本指南默认场景）
  - 在另一台机器上的 rag，能从本机 docker 网络内访问
  - 全新自起一套 rag 栈（参考 `docker/docker-compose.rag.yml`）
- 数据盘：MySQL + ES + Milvus + MinIO 空载约 5–10 GB；上传文档后会增长，建议预留 ≥ 50GB
- 内核 `fs.aio-max-nr` 充足（默认 64K，多实例可能不够，见 §8.1）

---

## 3. 从 RAG 侧拿到必填信息

```bash
docker exec <rag-web 容器名> cat /etc/rag/model_providers.json | python3 -m json.tool
```

从输出里挑三个 `model_id`：

| coze 环境变量 | 必须 | 来源 |
|---|---|---|
| `RAG_DEFAULT_TEXT_EMBEDDING_MODEL_ID` | ✅ | `type=text_embedding` 那条 |
| `RAG_DEFAULT_IMAGE_EMBEDDING_MODEL_ID` | ⭕ | `type=image_embedding` 那条；仅多模态用 |
| `RAG_DEFAULT_LLM_MODEL_ID` | ⭕ | `type=llm` + `capabilities` 含 `query_rewrite` 那条；不填则查询改写关闭 |

> RAG 没有 service-to-service 鉴权，靠 docker 网络隔离。所以无 token / api-key 字段需要配置。

---

## 4. 配置文件

### 4.1 `docker/.env`（基础）

```bash
cd <repo>/docker
cp .env.example .env
```

至少填好：
- `MODEL_PROTOCOL_0` / `MODEL_API_KEY_0` / `MODEL_ID_0` 等：默认聊天模型
- `BUILTIN_CM_*`：内置模型（NL2SQL、message2query 等）的 API key
- `ARK_EMBEDDING_*` 或对应 embedding 提供商：legacy 路径仍会读取，可空字符串占位

`.env` 是两套实例**共享**的基础变量。差异化的部分放到 `.env.v2`。

### 4.2 `docker/.env.v2`（并行实例覆盖）

```bash
# 对外地址（OAuth 回调 / 邮件链接生成用得到，指向前端 nginx）
export SERVER_HOST="http://<服务器 IP>:<WEB_PORT>"
export WEB_LISTEN_ADDR="0.0.0.0:<WEB_PORT>"

# 切到 RAG 后端
export KNOWLEDGE_BACKEND="rag"
export RAG_CONFIG_PATH="resources/conf/rag/rag.yaml"   # ← 不能省，见 §8.3
export RAG_BASE_URL="http://rag-web:8000"
export RAG_TENANT_MODE="env"
export RAG_TENANT_ID="<unique-tenant-id>"              # ← 例: coze-v2，不能跟旧实例重名

# 模型 ID（从 §3 拿到）
export RAG_DEFAULT_TEXT_EMBEDDING_MODEL_ID="..."
export RAG_DEFAULT_IMAGE_EMBEDDING_MODEL_ID=""
export RAG_DEFAULT_LLM_MODEL_ID=""
```

### 4.3 `docker/docker-compose.v2.yml`（并行实例 override）

```yaml
name: coze-studio-v2

x-env-file: &env_file
  - .env
  - .env.v2

services:
  mysql:
    container_name: coze-mysql-v2
    env_file: *env_file
    ports: !override
      - '<DB_PORT>:3306'
    volumes:
      - <DATA_DIR>/mysql:/var/lib/mysql
      - ./volumes/mysql/schema.sql:/docker-entrypoint-initdb.d/init.sql
      - ./atlas/opencoze_latest_schema.hcl:/opencoze_latest_schema.hcl:ro
      - ./volumes/mysql/my.v2.cnf:/etc/mysql/conf.d/zz-coze-v2.cnf:ro   # 见 §8.1

  redis:
    container_name: coze-redis-v2
    env_file: *env_file
    volumes:
      - <DATA_DIR>/bitnami/redis:/bitnami/redis/data:rw,Z

  elasticsearch:
    container_name: coze-elasticsearch-v2
    env_file: *env_file
    volumes:
      - <DATA_DIR>/bitnami/elasticsearch:/bitnami/elasticsearch/data
      - ./volumes/elasticsearch/elasticsearch.yml:/opt/bitnami/elasticsearch/config/my_elasticsearch.yml
      - ./volumes/elasticsearch/analysis-smartcn.zip:/opt/bitnami/elasticsearch/analysis-smartcn.zip:rw,Z

  minio:
    container_name: coze-minio-v2
    env_file: *env_file
    volumes:
      - <DATA_DIR>/minio:/data

  etcd:
    container_name: coze-etcd-v2
    env_file: *env_file
    volumes:
      - <DATA_DIR>/bitnami/etcd:/bitnami/etcd:rw,Z
      - ./volumes/etcd/etcd.conf.yml:/opt/bitnami/etcd/conf/etcd.conf.yml:ro,Z

  milvus:
    container_name: coze-milvus-v2
    env_file: *env_file
    volumes:
      - <DATA_DIR>/milvus:/var/lib/milvus:rw,Z

  nsqlookupd:
    container_name: coze-nsqlookupd-v2
    env_file: *env_file
  nsqd:
    container_name: coze-nsqd-v2
    env_file: *env_file
  nsqadmin:
    container_name: coze-nsqadmin-v2
    env_file: *env_file

  coze-server:
    container_name: coze-server-v2
    image: coze-studio-server-v2:latest
    env_file: *env_file
    ports: !override
      - '<API_PORT>:8888'
    networks:
      - coze-network
      - rag-net

  coze-web:
    container_name: coze-web-v2
    image: coze-studio-web-v2:latest
    build:
      args:                                # ← 见 §8.2，若 daocloud 镜像源 401
        NODE_IMAGE: node:22-alpine
        NGINX_IMAGE: nginx:1.25-alpine
    env_file: *env_file
    ports: !override
      - '<WEB_PORT>:80'

networks:
  rag-net:
    external: true
    name: <已有 rag 服务的网络名>          # ← 通常是 rag_default
```

要点：
- `!override` 让 `ports:` 替换基础文件的列表（默认是合并，会导致 `8888` 和 `<API_PORT>` 都暴露 → 跟旧实例冲突）
- `image: coze-studio-{server,web}-v2:latest` 让 v2 用独立镜像 tag，build 时不会污染旧 coze 的 `*-local:latest`
- `networks.rag-net.external + name` 把 v2 的 coze-server 接入已有 rag 服务的网络，DNS 直接解析 `rag-web`

### 4.4 `docker/volumes/mysql/my.v2.cnf`（解决 AIO 限制，见 §8.1）

```ini
[mysqld]
innodb_use_native_aio = 0
```

---

## 5. 启动

### 5.1 单实例（全新部署）

```bash
cd <repo>/docker
cp .env.example .env       # 编辑填好 model API key 等
docker compose -f docker-compose.yml -f docker-compose.override.yml \
  -p coze-studio up -d --build
```

### 5.2 并行实例（v2）

```bash
cd <repo>/docker

# 1) 准备数据目录
mkdir -p <DATA_DIR>/{mysql,minio,milvus} \
         <DATA_DIR>/bitnami/{redis,elasticsearch,etcd}

# 2) build 独立镜像（约 5–10 分钟）
docker compose \
  -f docker-compose.yml -f docker-compose.override.yml -f docker-compose.v2.yml \
  -p coze-studio-v2 build coze-server coze-web

# 3) 启动
docker compose \
  -f docker-compose.yml -f docker-compose.override.yml -f docker-compose.v2.yml \
  -p coze-studio-v2 up -d

# 4) 等 mysql 初始化 + Atlas migration（30s ~ 1min30s）
sleep 30 && docker compose -p coze-studio-v2 ps
```

第一次 mysql 启动的具体流程：mysqld 初始化 `<DATA_DIR>/mysql` → 跑 `schema.sql` bootstrap → 等 `workflow_version` 表出现 → 容器内 `curl atlasgo.sh` 装 Atlas CLI → `atlas schema apply` 把库推到 `opencoze_latest_schema.hcl` 描述的目标状态。

---

## 6. 验证

```bash
# 1) 11 个容器都健康
docker compose -p coze-studio-v2 ps

# 2) coze-server-v2 同时挂在两张网络
docker inspect coze-server-v2 \
  --format '{{range $k,$v := .NetworkSettings.Networks}}{{$k}} {{end}}'
# 期望含: coze-studio-v2_coze-network  和  rag_default

# 3) 容器内能 DNS + HTTP 访问 rag-web
docker exec coze-server-v2 sh -c 'wget -qO- http://rag-web:8000/ready'
# 期望: {"checks": {...}}

# 4) HTTP 端口对外可达
curl -s -o /dev/null -w 'web=%{http_code}\n' http://<服务器 IP>:<WEB_PORT>
curl -s -o /dev/null -w 'api=%{http_code}\n' http://<服务器 IP>:<API_PORT>

# 5) rag-web 日志能看到 v2 发起的请求
docker logs <rag-web 容器名> 2>&1 | grep -iE "tenant=<unique-tenant-id>" | tail -5
```

浏览器走一遍冒烟：注册账号 → 新建文本知识库（模型下拉框能看到 text embedding 选项）→ 上传文档（几秒内变"完成"）→ 新建 Agent 挂这个 KB → 提问看是否引用文档。

成功的检索链应该在 rag-web 日志里看到：

```
POST /v1/chat/completions      ← query rewrite (LLM)
POST /v1/embeddings             ← 向量化 (text embedding)
POST .../rag_<tenant>_<kb>/_search  ← ES 检索
POST /api/v1/retrieval 200 ~300ms
```

---

## 7. 日常运维

```bash
# 别名（建议加到 ~/.bashrc 简化）
alias dcv2='docker compose \
  -f /home/<user>/coze-studio/docker/docker-compose.yml \
  -f /home/<user>/coze-studio/docker/docker-compose.override.yml \
  -f /home/<user>/coze-studio/docker/docker-compose.v2.yml \
  -p coze-studio-v2'

dcv2 ps
dcv2 logs -f coze-server
dcv2 restart coze-server          # .env.v2 改完后
dcv2 stop                          # 软停，保留数据
dcv2 up -d                         # 启动
dcv2 up -d --build coze-server     # 改了 backend 代码后
dcv2 down                          # 停 + 删容器，保留数据卷
dcv2 down -v                       # 停 + 删容器 + 删卷
```

**备份**（停服后整体打包数据目录）：

```bash
dcv2 stop
sudo tar -czf /backup/coze-v2-$(date +%F).tar.gz -C $(dirname <DATA_DIR>) $(basename <DATA_DIR>)
dcv2 start
```

**彻底卸载**：

```bash
dcv2 down -v
sudo rm -rf <DATA_DIR>
docker rmi coze-studio-server-v2:latest coze-studio-web-v2:latest 2>/dev/null
```

---

## 8. 常见问题

### 8.1 MySQL 启动报 `io_setup() failed with EAGAIN` / `Cannot initialize AIO sub-system`

内核全局 AIO 配额耗尽（多实例 MySQL + 其他容器同时争抢）。两种修法：

**A. 调高内核限制（治本，需 sudo）**

```bash
sudo sysctl -w fs.aio-max-nr=1048576
echo "fs.aio-max-nr = 1048576" | sudo tee /etc/sysctl.d/99-coze.conf
```

**B. 让 v2 的 MySQL 关掉 native AIO（治标，不需要 sudo）**

挂 §4.4 的 `my.v2.cnf` 进去，关掉 `innodb_use_native_aio`。

修完之后必须**清掉半成品的数据目录**（InnoDB 中断初始化会留下坏的 ibdata1）：

```bash
dcv2 down
sudo rm -rf <DATA_DIR>/mysql && mkdir -p <DATA_DIR>/mysql
dcv2 up -d
```

### 8.2 build 报 `docker.m.daocloud.io/library/...: 401 Unauthorized`

`docker-compose.override.yml` 默认用了 daocloud 镜像源，可能不稳定。在 v2.yml 的 `coze-web.build.args` 里覆盖回原始 docker.io 名（已写在 §4.3 模板里）。验证 docker.io 是否可达：

```bash
docker pull nginx:1.25-alpine
```

如果连 docker.io 也不通，要把 build args 改成你的内网 mirror 或者预先 `docker pull` 把基础镜像拉到本地。

### 8.3 coze-server panic：`open conf/rag/rag.yaml: no such file or directory`

`backend/application/knowledge/init.go` 默认相对路径找 `conf/rag/rag.yaml`，但 docker 镜像里 WORKDIR 是 `/app`，配置 mount 在 `/app/resources/conf/`。**必须**在 `.env`（或 `.env.v2`）里设：

```bash
export RAG_CONFIG_PATH="resources/conf/rag/rag.yaml"
```

### 8.4 端口 publishing 既出现 `8888` 又出现 `<API_PORT>`（或 8890 + `<WEB_PORT>`）

`ports:` 在 compose 里默认**合并**，导致跟旧实例撞端口。在 v2.yml 的对应服务里把 `ports:` 改成 `ports: !override`（已写在 §4.3 模板里）。验证：

```bash
docker compose -p coze-studio-v2 -f ... config | grep -E "container_name:|published:"
```

每个服务下应该**只看到一个** published 端口。

### 8.5 v2 build 之后旧 coze 的镜像被改写

没给 v2 设独立 image tag，导致 `coze-studio-{server,web}-local:latest` 被新 build 覆盖。旧的正在运行的容器引用的是旧镜像 sha256，**不会立刻受影响**；但旧 coze 一旦 `up --force-recreate` 就会跳到新代码。

修法：v2.yml 里加 `image: coze-studio-{server,web}-v2:latest`（§4.3 模板里已有）。

### 8.6 coze-server 日志 `RAG_DEFAULT_TEXT_EMBEDDING_MODEL_ID is empty`

`.env.v2` 没填这个变量，或填的 id 在 rag 端不存在。回到 §3 重新拿一遍 id。

### 8.7 工作流检索节点报 `rag 40004: query_strategy.llm_model_id is required`

`RAG_DEFAULT_LLM_MODEL_ID` 未设置。要么填一个 `type=llm` 且 `capabilities` 含 `query_rewrite` 的 model id，要么在节点配置里关掉"查询改写"。

### 8.8 切片管理 / 文档元数据更新 / KB 复制 等 UI 报 `105100001 feature pending rag support`

预期行为。rag 不暴露 chunk-level API，coze 这些 UI 入口在 rag 后端下没意义。后续 PR 会屏蔽入口，当前忽略。

---

## 附录 A：原生 `make server` 开发模式（旧流程）

仅供本地开发调试，不推荐生产部署。要点：

- Go **1.24**（≥ 1.26 与 `bytedance/sonic v1.14` 不兼容）：`GOTOOLCHAIN=go1.24.0 make server`
- 前端改动后必须 `make fe`，然后 `pkill -f opencoze && make server`
- rag 服务一般起在同机 docker 里，coze-server 通过 `RAG_BASE_URL=http://localhost:8000` 访问
- rag MinIO bucket 首次要手动创建：
  ```bash
  docker run --rm --network rag_default --entrypoint sh minio/mc:latest -c \
    "mc alias set rag http://minio:9000 minioadmin minioadmin && mc mb rag/rag-files"
  ```
- pnpm store 报错（仓库目录搬动后）：`cd frontend && rush update --purge`

详细见 git 历史 `4949711e docs(rag): add Chinese progress summary and deployment guide` 中本文件的旧版本。

---

## 附录 B：相关文档

- 项目整体进度：[`docs/rag-replacement-progress-zh.md`](rag-replacement-progress-zh.md)
- coze vs rag feature gap：[`docs/rag-feature-gaps-zh.md`](rag-feature-gaps-zh.md)
- 各切片设计 spec：[`docs/superpowers/specs/`](superpowers/specs/)
- 各切片实现 plan：[`docs/superpowers/plans/`](superpowers/plans/)
