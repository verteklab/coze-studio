# R2-G 手动 chunk CRUD — 接口简表

**Date:** 2026-05-15（2026-05-15 更新：按 rag 已实现形状校准）
**配套完整 spec：** `2026-05-15-r2g-manual-slice-design.md`
**Rag 侧实现位置：** `rag/app/api/routes/chunks.py` · `rag/app/api/schemas/chunk.py`

公共约定：
- 所有 endpoint 走 `X-Tenant-Id` header 做租户隔离
- 响应统一 `ResponseEnvelope`：`{ "data": <T>, "request_id": "..." }`
- `kb_id` / `doc_id` / `chunk_id` 全部 string UUID
- **API 风格只允许 GET / POST**（rag 约定）；状态变更动作用 POST + 后缀路径（`/update`、`/delete`、`:mget`），不用 PUT / PATCH / DELETE
- 错误码：40004 参数 / 40404 not found / 40901 状态不允许 / 50001 内部
- 所有写动作（create / update / delete）**同步**完成（含 embed + ES 索引），不进任务队列

---

## 1. CreateChunk — 在指定文档下插入一个 chunk

`POST /api/v1/knowledgebases/{kb_id}/documents/{doc_id}/chunks`

**功能**：手动向已 ready 的 document 添加一个 chunk。同步完成 embed + ES 索引。`position.sequence_index` 缺省时 append 到末尾；指定时 rag 内部把 ≥N 的现有 chunk sequence 全部 +1。

Request body
```json
{
  "chunk_type": "text_chunk",            // text_chunk | image_chunk
  "content": "...",                       // text_chunk 必填
  "image": {                              // image_chunk 必填，且只能用 image_ref（不接受 base64）
    "image_ref": "minio://bucket/key",
    "ocr_text": "...",
    "ocr_used": true,
    "caption": ""
  },
  "position": { "sequence_index": 12 },   // 可选；缺省 append
  "metadata": { "creator_id": "...", "source": "manual" }
}
```

Response data（`ChunkDetail`）
```json
{
  "chunk_id": "550e8400-...",
  "doc_id": "doc-uuid",
  "kb_id": "kb-uuid",
  "doc_name": "design.pdf",               // rag 端直接挂上 doc_name，coze 无需二次回填
  "chunk_type": "text_chunk",
  "sequence_index": 12,
  "content": "...",
  "image": { "image_ref": "...", "image_url": "https://signed/...", "ocr_text": "...", "caption": "" },
  "char_count": 380,
  "byte_count": 612,
  "metadata": { "...": "..." },
  "source": { "...": "..." },             // ingestion 来源信息
  "position": { "...": "..." },
  "source_modality": "text_source",
  "source_ref": "minio://...",
  "modality_payload": { "...": "..." },
  "status": "ready",                      // ready | failed
  "created_at": "2026-05-15T08:21:00Z",
  "updated_at": "2026-05-15T08:21:00Z"
}
```

---

## 2. UpdateChunk — 编辑已存在的 chunk

`POST /api/v1/knowledgebases/{kb_id}/documents/{doc_id}/chunks/{chunk_id}/update`

**功能**：改 `content`、`metadata` 或 `image` 子字段。`content` 变化时 rag 用 KB 已绑定的 `text_embedding_model_id` 重新 embed 并 upsert 同一个 chunk_id（id 稳定）。只改 `metadata` / `image` 子字段时跳过 embed。空 `content` 字符串被拒（40004 "content cannot be empty"）。不接受 `chunk_type` / `position`。

Request body
```json
{
  "content": "edited text",                // 可选；非空字符串
  "metadata": { "tags": ["foo"] },         // 可选；full replace 该字段集
  "image": {                               // 可选；image_chunk 才有意义
    "image_ref": "minio://...",
    "ocr_text": "...",
    "ocr_used": true,
    "caption": "..."
  }
}
```

Response data：同 §1 的 `ChunkDetail`，含更新后的 `char_count` / `byte_count` / `updated_at`。

---

## 3. DeleteChunk — 删除一个 chunk

`POST /api/v1/knowledgebases/{kb_id}/documents/{doc_id}/chunks/{chunk_id}/delete`

**功能**：从 ES 删除 chunk + 更新 `documents.chunk_count -= 1`。不对 sequence renumber（gap 是良性的）。chunk 不存在 / 已被删 / 不属于该 doc 时返回 40404。

Response data
```json
{ "deleted": true }
```

---

## 4. ListChunksByDoc — 按文档列 chunk（带分页 / 关键词 / 类型筛选）

`GET /api/v1/knowledgebases/{kb_id}/documents/{doc_id}/chunks?page=1&page_size=20&keyword=&chunk_type=&after_sequence=`

**功能**：知识库详情页 → 文档详情 → chunk 列表的数据源。支持翻页（`page_size` 1..200）、关键词在 `content` 上做 ES `match`、按 chunk_type 过滤、按 sequence 游标。

Response data
```json
{
  "items": [ /* 元素结构同 §1 ChunkDetail */ ],
  "total": 137,
  "page": 1,
  "page_size": 20
}
```

---

## 5. GetChunk — 取单个 chunk

rag 提供两个等价 endpoint：

- `GET /api/v1/knowledgebases/{kb_id}/chunks/{chunk_id}`（KB 作用域 — coze 优先用这个，因 ragimpl 只持 `slice_id → chunk_id` 映射，未必预知 doc_id）
- `GET /api/v1/knowledgebases/{kb_id}/documents/{doc_id}/chunks/{chunk_id}`（doc 作用域 — 备用，rag 校验 chunk 确实属于该 doc）

**功能**：按 chunk_id 拉单条详情。响应里直接带 `doc_id` 和 `doc_name`。

Response data：同 §1 的 `ChunkDetail`。

---

## 6. MGetChunks — 批量取 chunk

`POST /api/v1/knowledgebases/{kb_id}/chunks:mget`

**功能**：UI 一次列 50–100 行 chunk 时批量回填详情。请求 body 里 `chunk_ids` 1..200 条；超过 / 含空字符串 → 40004。响应顺序与请求一致；已删的位置用占位返回，不打断顺序。不允许跨 KB（不同 kb 走多次调用）。

Request body
```json
{ "chunk_ids": ["uuid-1", "uuid-2", "..."] }
```

Response data
```json
{
  "items": [
    /* 命中：同 §1 ChunkDetail */
    { "chunk_id": "uuid-2", "deleted": true }    /* 已删占位 */
  ]
}
```

---

## 7. ListChunksByKB — KB 级 list（覆盖 `ListPhotoSlice` 等场景）

`GET /api/v1/knowledgebases/{kb_id}/chunks?chunk_type=image_chunk&doc_ids=a,b,c&keyword=&after_sequence=&page=&page_size=`

**功能**：跨整个 KB（或指定多个 doc）列 chunk。覆盖图片型 KB 的 "ListPhotoSlice" 用例（`chunk_type=image_chunk`），也覆盖未来跨文档浏览的需求。`doc_ids` 逗号分隔。

> ⚠️ **rag 当前未实现 `has_caption` 过滤**（gap 文档 §A 在 2026-05-15 校准后保留为 R2-L2 续做项）。coze 侧 `ListPhotoSliceRequest.HasCaption` 接到 rag 层时只能：
> - 不传给 rag，拿到结果后 coze 端 post-filter `Slice.RawContent[0].Image.Caption` 是否非空 —— **首选**（带宽损失较小，因 image chunks 数量通常 <数千）；
> - 或在 ragimpl 直接 drop 该字段并记 WARN（如同当前 `DocumentIDs` 的做法），等 rag 端实现。
> 建议两者皆放进 R2-G 实现里：能 post-filter 就 post-filter，不能就 drop + WARN。

Response data：同 §4。

---

## 错误码速查

| Rag code | 触发场景 | 用户感知 |
|---|---|---|
| `40004` | 参数不合法 / metadata 不在 schema / `chunk_ids` 超量或含空 / `content` 空字符串 | "invalid parameter" + rag message |
| `40404` | chunk_id 不存在或已删 | 404 |
| `40901` | doc 状态非 ready 时尝试 create / update / delete | "document is not ready, cannot edit chunks" |
| `50001` | embed 失败 / ES 不可写 | 500，旧 chunk 不变 |

---

## Coze 侧映射

每个 endpoint 在 coze 侧对应 `service.Knowledge` 的一个方法，由 `ragimpl/slice.go` 实现：

| Service 方法 | Rag endpoint |
|---|---|
| `CreateSlice` | §1 `POST .../chunks` |
| `UpdateSlice` | §2 `POST .../chunks/{chunk_id}/update` |
| `DeleteSlice` | §3 `POST .../chunks/{chunk_id}/delete` |
| `ListSlice` | §4 `GET .../documents/{doc_id}/chunks` |
| `GetSlice` | §5 `GET /knowledgebases/{kb_id}/chunks/{chunk_id}` |
| `MGetSlice` | §6 `POST /knowledgebases/{kb_id}/chunks:mget`（按 kb 分组，可能多次调用） |
| `ListPhotoSlice` | §7 `GET /knowledgebases/{kb_id}/chunks?chunk_type=image_chunk`（含 has_caption 的 post-filter / drop 策略，见 §7 备注） |

`int64 SliceID ↔ string chunk_id` 的翻译走 coze 新建的 `rag_chunk_mapping` 表（详见完整 spec §3.2 / §5.2 / §5.3）。
