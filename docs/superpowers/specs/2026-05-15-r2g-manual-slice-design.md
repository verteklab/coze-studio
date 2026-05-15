# R2-G: manual chunk CRUD (`CreateSlice` / `UpdateSlice` / `DeleteSlice` / `ListSlice` / `GetSlice` / `MGetSlice` / `ListPhotoSlice`)

**Date:** 2026-05-15（2026-05-15 更新：rag 端已落地，本 spec 重心调整为 coze 端 wiring）
**Status:** Ready for plan (coze-side only — rag endpoints已上线，见 `rag/app/api/routes/chunks.py`)
**Predecessor:** `2026-05-14-r2f-retrieval-llm-model-id-design.md` (R2-F)
**Sibling slices:** R2-H / R2-I / R2-J / R2-K / R2-L（小 wiring 项，本 slice 落地前后均可并行）；R2-F-Rerank（已排队）；R2-D-fe-Wizard（已排队）
**Companion gap:** `docs/rag-feature-gaps-zh.md` §A' 行 "切片手动管理"；§A' 同时把 "切片 chunk-level ID 稳定性"（原 §C 行）并入本 slice 一并解决
**Companion brief:** `2026-05-15-r2g-manual-slice-api-brief.md`（按 rag 实际形状校准的接口简表）

## 1. Motivation

`backend/domain/knowledge/service/ragimpl/unsupported.go:54-80` stubs out the seven manual-chunk methods on `service.Knowledge`. Every call returns `105100001 feature pending rag support`. The user-visible impact:

- 知识库详情页 → 文档 → "查看 / 编辑 / 增删 chunk" 入口全部失败。
- 图片型 KB 的 chunk 浏览页（`ListPhotoSlice`）同样失败。
- 检索命中结果里的 chunk 没有持久 ID 可以回链（`Slice.Info.ID` 永远是 0，见 `rag-feature-gaps-zh.md` §C 第 3 行）。后者不属于"手动 CRUD"功能本身，但和本 slice 共享底层映射表，**一并解决**。

**Rag 端在 2026-05-15 前已实现 7 个 endpoint**（`rag/app/api/routes/chunks.py`），形状与本 spec 早期草案略有差异（POST + `/update` / `/delete` 后缀；KB 级 list 暂未实现 `has_caption`）——以 brief 为接口契约的事实来源。本 spec 现在只描述 coze 端 wiring：用一张新的 `rag_chunk_mapping` 表把 coze 侧 `int64` slice id 翻译到 rag 侧 string `chunk_id`，模式完全照搬现有的 `rag_doc_mapping`（`backend/domain/knowledge/service/ragimpl/mapping.go`）。检索结果里的 chunk id 也借由同一张表回填，顺手填掉 §C 第 3 行的 `Slice.Info.ID=0` 窟窿。

## 2. Goals & non-goals

### Goals

- **Coze 端**：新建 `backend/domain/knowledge/service/ragimpl/slice.go` 实现 7 个方法，删除 `unsupported.go` 里对应的 stub。
- **Coze 端**：新建 `rag_chunk_mapping` 表（atlas migration），结构 `coze_slice_id BIGINT PK, rag_chunk_id VARCHAR(64), rag_doc_id VARCHAR(64), coze_doc_id BIGINT, deleted_at DATETIME(3) NULL`，加 `mapping.go` 里 `ChunkByCozeID / ChunkByRagID / ChunkInsert / ChunkSoftDelete / ChunksByCozeIDs` 五个方法。
- **Coze 端**：`backend/infra/contract/rag/client.go` 加 7 个 Client 方法 + DTO；`backend/infra/rag/client.go` 实现 HTTP 调用（包含 httptest 锁形）。
- **Coze 端**：`ragimpl.Retrieve` 在拼装 `entity.Slice` 时通过 `MappingRepo.ChunkByRagID` 回填 `Slice.Info.ID`，找不到映射的 chunk **当场 insert 一行**（懒映射）。这填掉 gap 文档 §C 第 3 行。
- Unit + integration test 覆盖所有 7 个方法 + 懒映射回填路径。

### Non-goals

- **表格类 chunk 的 manual CRUD**。`Slice.RawContent` 支持 `SliceContentType=Table`，但表格摄入本身是另一条 bucket-B 项（`GetAlterTableSchema` / `ValidateTableSchema` 等），等那条做完再合。本 slice 收到 `RawContent[i].Type == SliceContentTypeTable` 时返回 `ErrKnowledgeInvalidParam`，message 明示"manual table chunk CRUD pending"。
- **重排序（reorder）独立动作**。`UpdateSlice` 不允许改 `position`；如果产品要"上移 / 下移"，等列入 R2-G2 单独做。本 slice 的 `CreateSlice` 可以指定插入位置，足够覆盖现有 UI 的"在末尾追加"+"在某行前 / 后插入"按钮。
- **图片 chunk 的 OCR 重抽取**。`UpdateSlice` 对 image_chunk 只允许改 `metadata`（caption / tags），不允许改 image_ref；前者无需重 embed。换图等同于删 + 建。
- **跨 KB 的 MGetSlice**。`MGetSliceRequest{SliceIDs []int64}` 在 coze 入参层没带 kb_id，但 ragimpl 通过 mapping 表能查出每个 slice 所属的 kb，按 kb 分组分批调 rag。
- **KB-level 重 embed**。本 slice 涉及的 update 操作只重 embed **单个 chunk**，使用该 KB 已绑定的 `text_embedding_model_id`。rag 侧 CLAUDE.md 的 "re-vectorization is not supported" 禁令针对换模型对整个 KB 重 embed，与此不冲突。
- **Frontend 改动**。前端"查看 / 编辑 chunk"组件已经在调 coze 的 slice service；本 slice 改完后这些调用自然走通。

## 3. Contract change

> §3.1 起原文的"NEW rag endpoint 设计"段保留作为 coze 端要消费的契约清单，便于 plan / 实现时对照。但所有 endpoint **已在 rag 端实现**，本 spec 不再要求 rag 团队按这里的提案重做。如发现 rag 实际形状与本节描述不一致，**以 rag 代码 + brief 为准**，回头修正本节。

### 3.1 Rag HTTP endpoints (已实现 — 形状以 brief 与 `rag/app/api/routes/chunks.py` 为准)

所有 endpoint：
- 走 `X-Tenant-Id` header 隔离
- 走 `ResponseEnvelope[T]` 包装（`{data, request_id}`，沿用 `app/api/schemas/common.py`）
- `chunk_id` / `doc_id` / `kb_id` 全部 string UUID
- 错误码遵循 `总体设计文档.md` §12.6（40001-40009 参数 / 4040x not-found / 40901 status 不允许操作 / 50001 内部）

#### 3.1.1 `POST /api/v1/knowledgebases/{kb_id}/documents/{doc_id}/chunks` — 创建 chunk

Request body:
```json
{
  "chunk_type": "text_chunk",
  "content": "...",
  "image": {
    "image_ref": "minio://bucket/object_key",
    "ocr_text": "...",
    "ocr_used": true
  },
  "position": {
    "sequence_index": 12
  },
  "metadata": {
    "creator_id": "7384...",
    "source": "manual"
  }
}
```

- `chunk_type` 必填，`text_chunk | image_chunk`。
- `content` 在 `chunk_type=text_chunk` 时必填，否则忽略。
- `image` 在 `chunk_type=image_chunk` 时必填，至少给 `image_ref`；`ocr_text` / `ocr_used` 可选。
- `position.sequence_index` 可选；缺省 = append 到末尾。若指定且小于当前 chunk 总数，rag 内部把 ≥ sequence_index 的现有 chunk 的 sequence 全部 +1（事务内）。
- `metadata` 必须满足 KB 的 `metadata_schema`（CLAUDE.md non-negotiable #9）；不满足的字段返回 40004。

Response data:
```json
{
  "chunk_id": "550e8400-e29b-41d4-a716-446655440000",
  "doc_id": "doc-uuid",
  "kb_id": "kb-uuid",
  "chunk_type": "text_chunk",
  "sequence_index": 12,
  "content": "...",
  "image": { "image_ref": "...", "image_url": "https://signed/...", "ocr_text": "...", "caption": "" },
  "char_count": 380,
  "byte_count": 612,
  "metadata": { "creator_id": "...", "source": "manual" },
  "created_at": "2026-05-15T08:21:00Z",
  "updated_at": "2026-05-15T08:21:00Z",
  "status": "ready"
}
```

- `status` 取值 `ready | failed`。同步路径：rag 在请求内完成 embed → ES upsert → 返回 `ready`；embed 或 ES 失败时返回 `failed` 且 4xx/5xx 错误码。
- `image_url` 仅 image_chunk 有；text_chunk 时整个 `image` 字段省略。
- Doc 必须处于 `ready` 状态，否则返回 40901（不允许在 pending/processing/failed 文档上手动加 chunk）。

#### 3.1.2 `PUT /api/v1/knowledgebases/{kb_id}/documents/{doc_id}/chunks/{chunk_id}` — 更新 chunk

Request body（与 create 同字段；缺省 = 不改）：
```json
{
  "content": "edited text",
  "metadata": { "tags": ["foo"] }
}
```

- 不允许在 update 时改 `chunk_type` / `position` / `image.image_ref`。前两者返回 40004；后者按 §2 non-goals 处理。
- 当 `content` 变化时，rag **必须**用 KB 绑定的 `text_embedding_model_id` 对新文本重新 embed，并 ES upsert 同一个 chunk_id（保 chunk_id 稳定）。
- 当只改 `metadata` 时，跳过 embed，仅 ES partial update。

Response data: 同 3.1.1，含更新后的 `char_count` / `byte_count` / `updated_at`。

#### 3.1.3 `DELETE /api/v1/knowledgebases/{kb_id}/documents/{doc_id}/chunks/{chunk_id}` — 删除 chunk

Response data: `{ "deleted": true }`。

- rag 删 ES 文档 + 更新 `documents.chunk_count` -1。
- 不做 sequence renumber——后续 ListChunks 直接按 sequence 排序时，gap 是良性的。
- 404 当 chunk 不存在 / 已被删 / 不属于该 doc。

#### 3.1.4 `GET /api/v1/knowledgebases/{kb_id}/documents/{doc_id}/chunks` — 按文档列 chunk

Query params: `page=1&page_size=20&keyword=&chunk_type=&after_sequence=`

- `chunk_type` 可选，缺省返回全部类型。
- `keyword` 可选，对 `content` 做 ES `match`。
- `after_sequence` 可选；优先级高于 `page`（cursor 模式）；同时给则报 40004。

Response data:
```json
{
  "items": [ /* 同 3.1.1 response data 数组 */ ],
  "total": 137,
  "page": 1,
  "page_size": 20
}
```

#### 3.1.5 `GET /api/v1/knowledgebases/{kb_id}/chunks/{chunk_id}` — 取单个 chunk

挂在 KB 下（而非 doc 下），因为 coze ragimpl 只持有 `slice_id → chunk_id` 的映射，不一定提前知道 doc。rag 服务端从 ES 反查出 doc_id 一并返回。

Response data: 同 3.1.1 response data 形状。

#### 3.1.6 `POST /api/v1/knowledgebases/{kb_id}/chunks:mget` — 批量取 chunk

用 POST 而非 GET 是因为 chunk_id 列表可能很长（UI 一次列 50-100 行）。

Request body:
```json
{ "chunk_ids": ["uuid-1", "uuid-2", "..."] }
```

Response data:
```json
{ "items": [ /* 顺序与请求一致；缺失的位置返回 {chunk_id, deleted: true} 占位 */ ] }
```

- 单次最多 200 条；超过返回 40004。
- 不允许跨 KB（不同 kb 走多次调用）。

#### 3.1.7 `GET /api/v1/knowledgebases/{kb_id}/chunks` — KB 级 list（覆盖 `ListPhotoSlice`）

Query params: `chunk_type=image_chunk&doc_ids=a,b,c&has_caption=true&page=&page_size=`

- 实质是 3.1.4 的 KB-scope 版本 + filter 加强。
- `doc_ids` 用逗号分隔，无该参数则跨整 KB。
- `has_caption=true` → `metadata.caption` 字段非空且非纯空格；`false` → 字段为空或缺失；不传则不 filter。

Response data: 同 3.1.4。

### 3.2 New mapping table (coze)

文件：`backend/domain/knowledge/service/ragimpl/migrations/0003_rag_chunk_mapping.sql`（具体路径以现有 atlas migration 目录为准；plan-time 确认）

```sql
CREATE TABLE rag_chunk_mapping (
  coze_slice_id BIGINT PRIMARY KEY,
  coze_doc_id   BIGINT       NOT NULL,
  rag_chunk_id  VARCHAR(64)  NOT NULL,
  rag_doc_id    VARCHAR(64)  NOT NULL,
  created_at    DATETIME(3)  NOT NULL DEFAULT NOW(3),
  deleted_at    DATETIME(3)  NULL,
  INDEX idx_rag_chunk_id (rag_chunk_id),
  INDEX idx_coze_doc_id (coze_doc_id, deleted_at),
  INDEX idx_rag_doc_id (rag_doc_id, deleted_at)
);
```

`MappingRepo` 新增方法（mapping.go 追加）：

| 方法 | 签名 | 用途 |
|---|---|---|
| `ChunkByCozeID` | `(ctx, cozeSliceID int64) (*ChunkMapping, error)` | UpdateSlice / DeleteSlice / GetSlice 入口翻译 |
| `ChunkByRagID` | `(ctx, ragChunkID string) (*ChunkMapping, error)` | Retrieve 命中结果回填 |
| `ChunksByCozeIDs` | `(ctx, cozeSliceIDs []int64) ([]*ChunkMapping, error)` | MGetSlice 入口翻译 |
| `ChunksByCozeDocID` | `(ctx, cozeDocID int64) ([]*ChunkMapping, error)` | ListSlice 内部对 chunk_id 批量回填（如果 rag 返回顺序与 sequence 一致，可跳过；保留方法以备 List 重排序场景） |
| `ChunkInsert` | `(ctx, m *ChunkMapping) error` | CreateSlice 成功后写入 |
| `ChunkSoftDelete` | `(ctx, cozeSliceID int64) error` | DeleteSlice 成功后软删 |
| `ChunkInsertOrGetCozeID` | `(ctx, ragChunkID, ragDocID string, cozeDocID int64) (int64, error)` | Retrieve / List 路径"懒映射"：rag 返回了未知 chunk_id 时用 idgen 分配 int64 并 insert，遇主键冲突读回已有行 |

`ChunkInsertOrGetCozeID` 必须用 `INSERT ... ON DUPLICATE KEY UPDATE rag_chunk_id=rag_chunk_id` 配合二次 SELECT，避免并发首次回填时插入两条不同 `coze_slice_id` 对应同一 `rag_chunk_id`。`rag_chunk_id` 列上的普通索引（非 UNIQUE）已足够——业务上同一 rag chunk 只允许有一条活跃 coze 映射，并发竞争用乐观读 + 二次校验解决（见 §5.4）。

### 3.3 `rag.Client` interface extensions

`backend/infra/contract/rag/client.go` 追加：

```go
type Client interface {
    // ... existing methods ...

    // Chunks (manual CRUD). All methods take tenantID and the relevant kb/doc ids.
    CreateChunk(ctx context.Context, tenantID, kbID, docID string, req *CreateChunkRequest) (*Chunk, error)
    UpdateChunk(ctx context.Context, tenantID, kbID, docID, chunkID string, req *UpdateChunkRequest) (*Chunk, error)
    DeleteChunk(ctx context.Context, tenantID, kbID, docID, chunkID string) error
    ListChunks(ctx context.Context, tenantID, kbID, docID string, req *ListChunksQuery) (*ListChunksResponse, error)
    GetChunk(ctx context.Context, tenantID, kbID, chunkID string) (*Chunk, error)
    MGetChunks(ctx context.Context, tenantID, kbID string, chunkIDs []string) (*MGetChunksResponse, error)
    ListChunksByKB(ctx context.Context, tenantID, kbID string, req *ListChunksByKBQuery) (*ListChunksResponse, error)
}
```

DTO（同包内新文件 `backend/infra/contract/rag/chunk.go`）按 §3.1 的 JSON 形状一一对应。`Chunk` 是统一的返回 DTO（同 §3.1.1 response data）。

### 3.4 `service.Knowledge` interface — 无改动

`CreateSliceRequest` / `UpdateSliceRequest` / 等 8 个结构（interface.go:127-217、355-381）形状已经够用。ragimpl 实现内部做翻译，不污染上层。

### 3.5 `entity.Slice.Info.ID` 填法

`backend/domain/knowledge/service/ragimpl/retrieval.go` 在拼装 `RetrieveSlice` 时插一个步骤：

```
for each rag hit:
  slice := buildSliceFromHit(hit)              // 现有逻辑
  cozeID, err := mappingRepo.ChunkInsertOrGetCozeID(ctx,
      hit.ChunkID, hit.DocID, cozeDocIDFromMapping)
  if err == nil {
      slice.Info.ID = cozeID
  }
  // 失败不阻断检索；记 WARN
```

同样的回填也用在 ListSlice / GetSlice / MGetSlice / ListPhotoSlice 的响应路径上——这些路径返回的 chunk_id 全部需要稳定的 int64 表示。

## 4. Architecture

### 4.1 Flow（以 CreateSlice 为例）

```
UI → coze API handler → service.Knowledge.CreateSlice(req: CreateSliceRequest{int64 ids, []SliceContent})
  → ragimpl.CreateSlice
    → mapping.DocByCozeID(req.DocumentID) → DocMapping{rag_doc_id, rag_kb_id}
    → translateRawContentToChunkPayload(req.RawContent) → (chunk_type, content/image, metadata)
    → rag.Client.CreateChunk(tenantID, kbID, docID, payload)
      → POST /api/v1/knowledgebases/{kb_id}/documents/{doc_id}/chunks
        → rag: embed inline → ES upsert → return Chunk
    ← Chunk{rag_chunk_id, ...}
    → idgen.NextID() → cozeSliceID int64
    → mapping.ChunkInsert(ChunkMapping{cozeSliceID, ragChunkID, ragDocID, cozeDocID})
    → return CreateSliceResponse{SliceID: cozeSliceID}
  ← cozeSliceID
← 200
```

UpdateSlice / DeleteSlice / GetSlice / MGetSlice 对称。ListSlice / ListPhotoSlice 走 KB-scope list endpoint，rag 返回的每个 chunk 通过 `ChunkInsertOrGetCozeID` 回填。

### 4.2 Rag 侧 5-layer 落点

按 rag CLAUDE.md §"Architecture: the 5 layers"：

| Layer | 新增 / 改动 |
|---|---|
| `app/api/schemas/` | 新 `chunk.py`：`ChunkDTO`, `CreateChunkRequest`, `UpdateChunkRequest`, `ListChunksResponse` |
| `app/api/routes/` | 新 `chunks.py`：挂 §3.1 的 7 个 endpoint |
| `app/api/deps/services.py` | 新增 `get_chunk_service` 依赖 |
| `app/services/` | 新 `chunk_service.py`：5 个公开方法（create / update / delete / get / list / mget），用同一 `ResolvedIngestionPolicy.text_embedding_model_id` 触发单 chunk re-embed |
| `app/orchestrators/` | **无新增**——manual CRUD 是单步原子动作，不走 orchestrator |
| `app/executors/` | 复用现有 `EmbeddingExecutor` / `IndexExecutor`；新增 `IndexExecutor.delete_one` / `upsert_one` 若不存在 |
| `app/infrastructure/db/repositories/chunk_index_repository.py` | 新增 `delete_by_chunk_id` / `upsert_one_chunk` / `mget_by_ids` / `list_by_doc` / `list_by_kb_with_filters`；filter 字段必须经过 KB `metadata_schema` 校验（CLAUDE.md non-negotiable #9） |

写动作全部在请求线程内完成；不入 Celery 队列。`ChunkService.update_with_reembed` 在事务边界外执行 embed 调用（embed 不可回滚），但 ES upsert 是幂等的，failure 时 service 抛 5xx 并保留旧 chunk 不动。

### 4.3 Files touched

**Rag side:** 已在 2026-05-15 之前由 rag 团队实现，本 slice 不动 rag 代码。验收清单：
- `rag/app/api/routes/chunks.py` 存在且包含 §3.1 列出的 7 个 endpoint（已核验 ✅）
- `rag/app/api/schemas/chunk.py` 存在且 DTO 字段如 brief 所示（已核验 ✅）
- `GET /knowledgebases/{kb_id}/chunks` 未实现 `has_caption` query 参数 → coze 端按 brief §7 备注做 post-filter（已核验 ✅）

**Coze side（本 slice 工作面）：**

| File | Change |
|---|---|
| `backend/domain/knowledge/service/ragimpl/slice.go` | NEW — 7 个 service.Knowledge 方法实现 |
| `backend/domain/knowledge/service/ragimpl/unsupported.go:54-80` | 删除 7 个 stub |
| `backend/domain/knowledge/service/ragimpl/mapping.go` | 加 `ChunkMapping` struct + 7 个 method（§3.2 表格） |
| `backend/domain/knowledge/service/ragimpl/migrations/...` | NEW atlas migration（§3.2 SQL） |
| `backend/domain/knowledge/service/ragimpl/retrieval.go` | 在结果拼装处插 `ChunkInsertOrGetCozeID` 回填 Slice.Info.ID |
| `backend/infra/contract/rag/client.go` | 加 7 个 Client 方法 |
| `backend/infra/contract/rag/chunk.go` | NEW — DTO |
| `backend/infra/rag/client.go` | 实现 7 个 HTTP 调用 |
| `backend/infra/rag/client_test.go` | 加 httptest 锁形 case |
| `backend/domain/knowledge/service/ragimpl/slice_test.go` | NEW — 7 个方法 happy / err / 懒映射 |
| `backend/domain/knowledge/service/ragimpl/mapping_test.go` | 加 `ChunkMapping` 相关 case |
| `backend/domain/knowledge/service/ragimpl/integration_test.go` | 加跨 7 个方法的端到端 smoke（build-tag-gated） |
| `backend/domain/knowledge/service/ragimpl/retrieval_test.go` | 加 chunk id 回填 case |
| `backend/domain/knowledge/service/ragimpl/unsupported_test.go` | 删 7 个对应的 stub 期望 |
| `docs/rag-feature-gaps-zh.md` | §A 删 "切片手动管理" 行；§C 删 "切片 chunk-level ID 稳定性" 行；§D 增对应行 |

## 5. Components

### 5.1 同步 vs 异步选择

Document upload 是异步的，因为单文档 embed + 切块 + ES upsert 可能 30s-数分钟。Single chunk 的 embed 在 OpenAI/本地模型上稳定 < 1s；走异步会让 UI 出现 "保存按钮按完还要轮询" 的笨拙体验。同步路径的代价是把 embed 失败的故障域揉进了 HTTP 请求，但相比 ingestion 任务，故障面小、可重试，且 UI 直接看到错误码更容易诊断。

如果未来观测到 p95 超 3s，可以平滑转异步：rag 响应里加 `task_id` 字段（沿用 UploadDocumentResponse 形状），coze 端引入 polling。届时再起 R2-G2。

### 5.2 chunk_id 类型在 coze 内部的边界

`entity.Slice.Info.ID int64` 是 service 接口和 UI handler 之间的契约，**不动**——动了会把整个 knowledge domain 的类型系统翻一遍。所以选 mapping-table 路线而不是把 ID 改 string。

代价：每次回填多一次 DB 读/写。读路径用 `IN (...)` 批量；写路径只在 chunk 首次出现时发生一次，之后命中索引。预期 QPS 远低于检索本身，可忽略。

### 5.3 懒映射 vs 同步映射

`CreateSlice` 路径是同步映射——coze 主动新建 chunk，知道 rag 返回的 chunk_id，立刻 insert mapping。

`Retrieve` / `ListSlice` 等读路径需要懒映射：rag 可能返回的 chunk 是通过 document upload 流程产生的（不经过 manual CRUD），coze 此前从未见过。这些 chunk 在 ES 里存在但在 mapping 表里没有行。第一次被读到时，`ChunkInsertOrGetCozeID` 用 idgen 分配 int64 并 insert；后续读直接命中。

并发情况：两个 retrieve 请求并发命中同一未映射 chunk 时，两边都尝试 insert。设计上不在 `rag_chunk_id` 上加 UNIQUE 约束（避免主键冲突阻塞高 QPS 检索）；改用乐观读 → insert → 主键冲突时 fallback 到 select 再读出已存在的 `coze_slice_id`。短窗口内可能产生两条 `coze_slice_id` 不同但 `rag_chunk_id` 相同的行——读路径以最早 `created_at` 那条为权威，旧 id 永远稳定。详细的去重清理（如果业务上需要严格一对一）放到 plan-time 决定，本 spec 选择"宽松一对多 + 读最早"是因为 UI 不依赖唯一性，只依赖稳定性。

### 5.4 RawContent 翻译规则

`Slice.RawContent` 是 `[]*SliceContent`，每个有 `Type ∈ {Text, Table}`（注意 `Image` 类型在 `knowledge.go:223-225` 被注释掉了；图片 chunk 是整 slice 维度而非 content 子项维度）。Manual CRUD 场景下的翻译：

| RawContent shape | rag chunk_type | rag payload |
|---|---|---|
| 单元素，`Type=Text` | `text_chunk` | `content = RawContent[0].Text` |
| 多元素，全 `Type=Text` | `text_chunk` | `content = strings.Join(allTexts, "\n")` |
| 任一元素 `Type=Table` | reject (40004 manual table CRUD pending) | — |
| 空 | reject (40004) | — |

判断 image_chunk 不走 `RawContent`，走 `entity.Slice` 的 `RawContent[0].Image` 字段（即 `SliceContent.Image *SliceImage`）。Image chunk 在 CreateSlice 入参里必须有且仅有一个 `SliceContent`，其 `Image` 非 nil。

### 5.5 错误码映射

| Rag code | Coze 错误 | 用户感知 |
|---|---|---|
| 40004 metadata 不符合 schema | `ErrKnowledgeInvalidParam`（R2-C decoder 已覆盖） | "metadata field X not in KB schema" |
| 40004 manual table chunk pending | `ErrKnowledgeInvalidParam` | "table chunk manual CRUD not supported" |
| 40404 chunk not found | `ErrKnowledgeNotFound`（如不存在则在 R2-C decoder 加映射） | 404 |
| 40901 doc status not ready | `ErrKnowledgeStatusConflict`（如不存在则新加） | "document is not ready, cannot edit chunks" |
| 50001 embed 失败 | `ErrKnowledgeRagInternal` | 通用 500 + decoder 透出 rag message |

R2-C decoder 当前覆盖度需在 plan-time 验证；缺的映射在本 slice 内补。

## 6. Testing

### 6.1 Rag unit + integration

- `tests/unit/services/test_chunk_service.py`：
  - create + embed 成功 → ES upsert + chunk_count++
  - update content → embed 再调一次 + ES upsert（chunk_id 不变）
  - update metadata only → 不调 embed
  - delete → ES delete + chunk_count--
  - update with chunk_type change → 40004
  - update image_chunk image_ref → 40004
  - create on non-ready doc → 40901
- `tests/integration/test_chunks.py`：
  - 7 个 endpoint 各跑 happy 流（含 tenant_id header）
  - mget 跨 KB → 拒绝
  - chunks?has_caption=true 过滤生效
  - delete + 再 get → 40404

### 6.2 Coze unit

- `ragimpl/slice_test.go`：每个方法
  - happy：mapping 翻译 → fake client → mapping 回写 → 返回 int64
  - rag 404 → coze 透传错误
  - mapping 表读失败 → 错误
  - 懒映射（仅 List/Get/MGet）：rag 返回的 chunk_id 不在 mapping → insert → 第二次同 chunk_id 命中已有行
- `ragimpl/mapping_test.go`：`ChunkInsert` / `ChunkByCozeID` / `ChunkByRagID` / `ChunksByCozeIDs` / `ChunkSoftDelete` / `ChunkInsertOrGetCozeID` 并发冲突路径
- `ragimpl/retrieval_test.go`：检索结果回填 `Slice.Info.ID` 非 0（gap 文档 §C 第 3 行修复验证）

### 6.3 Coze httptest（infra/rag/client_test.go）

为每个 Client 方法增加一对锁形 case：
- 成功响应 → 解出 DTO 字段齐全
- rag 错误响应（40004 / 40404 / 40901）→ 返回 typed error

### 6.4 Integration smoke

`backend/domain/knowledge/service/ragimpl/integration_test.go` 加一个端到端 case（build-tag-gated）：
1. CreateKnowledge → CreateDocument → 等 doc 进 ready
2. CreateSlice 在末尾追加一个 text chunk
3. ListSlice 应见到新 chunk，且 `Slice.Info.ID` 非 0
4. GetSlice / MGetSlice 同 id 命中
5. UpdateSlice 改 content，GetSlice 内容更新且 id 不变
6. Retrieve 用相关 query → 命中结果包含该 chunk（`Slice.Info.ID` 与上面同）
7. DeleteSlice → 再 GetSlice → ErrKnowledgeNotFound

### 6.5 Smoke (human)

`docker compose up` → UI 进入 KB 详情 → 文档 → "查看 chunk" 应能列出 → 编辑某个 chunk 保存 → 内容立刻刷新 → 删除 → 列表少一条 → 重新检索同关键词 → 不再命中。失败时记 monitor 与 rag 日志。

## 7. Failure modes

| Scenario | Behavior |
|---|---|
| rag 端 embed 模型暂时不可用 | rag 50001 → coze `ErrKnowledgeRagInternal`，UI 错误提示用户重试；旧 chunk 不动（update 场景）。 |
| ES 暂时不可写 | rag 50001 → 同上。CreateSlice 时不会有"脏" mapping，因为 mapping insert 在 rag 成功响应之后。 |
| coze idgen 故障 | CreateSlice 在 rag 成功后 mapping insert 拿不到 cozeSliceID → 5xx，**rag 侧 chunk 已存在**。次回 ListSlice 走懒映射兜底，无业务影响（chunk 不会丢）。 |
| mapping 表与 rag ES 不一致（人工干扰 / 灾难恢复） | 懒映射兜底；删除路径 `DeleteSlice` 在 mapping 不存在时返回 ErrKnowledgeNotFound 而非冒进调 rag。 |
| KB 的 `text_embedding_model_id` 在 KB 创建后被人为篡改 | 违反 CLAUDE.md non-negotiable #2，不在本 slice 防御范围；rag 应保证 immutable。 |
| Doc 处于非 ready 状态（pending / processing / failed） | rag 40901 → coze 透出错误。UI 应在 doc 状态非 ready 时灰化"编辑 chunk"按钮（前端工作，已在 gap 文档 §C 第 4 行排队）。 |
| `RawContent` 含 SliceContentTypeTable | ragimpl 在调 rag 前就拒绝（40004），不浪费 rag 请求。 |
| MGetSlice 跨 KB | ragimpl 按 mapping 表的 kb_id 分组，发多次 rag mget；任一失败则全失败（不做部分成功）。 |
| 并发 update 同一 chunk | rag 侧 ES 是 last-write-wins。CLAUDE.md 没有约定乐观锁；本 slice 不引入，遵循现状。如果产品上需要，独立 slice 加 `If-Match: etag` 机制。 |

## 8. Compatibility & rollout

- **Coze**：legacy mode（非 rag backend）不受影响——所有改动在 ragimpl 包内。
- **Rag**：新增 endpoint 完全 additive；现有 document / retrieval / task 路由零改动。
- **DB**：coze 加一张新表，无现有表 schema 变化。
- **IDL**：service.Knowledge 接口形状未变；上层（UI handler / workflow node）零改动。
- **Frontend**：本 slice 完成后，前端"查看 / 编辑 / 增删 chunk" 按钮自然恢复。Gap 文档 §C 第 4 行 "Bucket-B UI 入口屏蔽" 仍然成立——其他未解决的 bucket-B 项（表格 / review / copy）依然要按 `kb.backend === "rag"` 屏蔽，本 slice 不替代那个工作。
- **滚动顺序**：先 deploy rag（向后兼容），再 deploy coze（含 mapping 表 migration）。反过来会导致 coze 调 rag 报 404 endpoint。

## 9. Open questions

rag 端已落地，原 Q1–Q5（sequence 并发 / renumber / mget 上限 / has_caption schema 校验 / processing_summary 回写）转为 rag 行为观察问题，plan 阶段用一个集成 smoke 跑一遍验证 rag 实际语义，文档化在 brief 中，无需阻塞实现。

剩余 coze-only open items：

1. **`ListPhotoSlice.HasCaption` 的处理策略**。rag 端 `GET /knowledgebases/{kb_id}/chunks` 不接受该过滤，coze 端有两个选择：
   - (a) post-filter：先按 `chunk_type=image_chunk` 拉一批，再在 ragimpl 里按 `Image.Caption` 非空筛选；
   - (b) drop + WARN：完全沿用 `DocumentIDs` 在 R2-I 落地前的做法。

   建议默认 (a)（语义正确），并在拉取数量超过某阈值时降级到 (b) + WARN。具体阈值 plan-time 定（建议 200，与 mget 上限一致）。
2. **HTTP timeout / retry policy**。同步 embed 在 OpenAI 慢网时可能拖 5s+；ragimpl 的默认 HTTP timeout 是否够？plan 时读现有 `client.go` 决定。
3. **R2-C 错误 decoder 的新增映射**。`40901 doc status conflict` 与 `40404 chunk not found` 如未注册，本 slice 内补。
4. **lazy mapping 的清理策略**。`ChunkInsertOrGetCozeID` 在并发场景下可能短窗口内产生两条不同 `coze_slice_id` 指向同一 `rag_chunk_id`（详见 §5.3）。本 slice 选"宽松一对多 + 读最早"。如果产品上需要严格唯一，作为 R2-G2 单独做一个 dedup 后台任务。
