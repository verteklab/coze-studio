# R2-I: wire `Retrieve` 的 `DocumentIDs` 过滤到 rag 的 `document_ids` 字段

**Date:** 2026-05-15
**Status:** Draft
**Predecessor:** R2-F (2026-05-14)
**Sibling slices:** R2-H / R2-J / R2-K / R2-L / R2-F-Rerank / R2-G —— 同期可并行
**Companion gap:** `docs/rag-feature-gaps-zh.md` §A' 行 "检索 `DocumentIDs` 过滤"

## 1. Motivation

`backend/domain/knowledge/service/ragimpl/retrieval.go:68-76` 在收到 `req.DocumentIDs` 时只记 WARN 后丢弃：

```go
if len(req.DocumentIDs) > 0 {
    logs.CtxWarnf(ctx, "ragimpl.Retrieve: DocumentIDs scoping requested but rag /retrieval does not yet support doc-level filters; ignoring %d ids", len(req.DocumentIDs))
}
```

用户语义损失：KB 内"只在选定文档中搜"完全失效，命中范围是整 KB。

Rag 端已在 `RetrievalRequest` 顶层接受 `document_ids: list[string]`（`rag/app/api/schemas/retrieval.py:35`），最多 200 条，含 dedup + 空值 reject 校验。R2-I 把 coze 侧 int64 doc id 通过 `rag_doc_mapping` 翻成 string，写入 `ragReq.DocumentIDs`，删 WARN。

## 2. Goals & non-goals

### Goals

- `backend/infra/contract/rag/types.go::RetrieveRequest` 加字段 `DocumentIDs []string \`json:"document_ids,omitempty"\``；更新原 line 230-233 stale 注释。
- `backend/domain/knowledge/service/ragimpl/retrieval.go:68-76` 改为：用 `mapping.DocsByCozeIDs(req.DocumentIDs)` 批量翻译，结果写 `ragReq.DocumentIDs`，删 WARN。
- 缺失 mapping 的 doc id 在结果集中**静默 drop**（不阻断检索；与现有 retrieve 命中处理一致）。
- 翻译后超过 200 时返回 `ErrKnowledgeInvalidParam`（rag 会 422 拒，提前在 coze 端报，错误信息更清晰）。
- Unit test：translate happy + 部分 mapping 缺失 + 超量拒绝 + 空列表（fall through 不设字段）。
- httptest：lock POST body 含 `document_ids` 字段。

### Non-goals

- 跨 KB 的 doc id 翻译合法性检查。`req.DocumentIDs` 都应属于 `req.KnowledgeIDs` 范围内的 KB，本 slice 不做交叉验证（rag 端会 silently 忽略不属于 kb 的 doc id）。
- 重新设计 service 接口。`RetrieveRequest.DocumentIDs []int64` 形状不变。

## 3. Contract change

### 3.1 RetrieveRequest 加字段

`backend/infra/contract/rag/types.go`：

```go
type RetrieveRequest struct {
    KBIDs            []string       `json:"kb_ids"`
    Query            *string        `json:"query,omitempty"`
    // ... existing fields ...
    DocumentIDs      []string       `json:"document_ids,omitempty"`  // NEW
    Filters          map[string]any `json:"filters,omitempty"`
    // ... rest ...
}
```

同时删除/更新当前注释（line 230-233）："Doc-level filtering is intentionally not exposed... rag's /retrieval endpoint has no `doc_ids` parameter" —— 这条已不成立。

### 3.2 ragimpl.Retrieve 翻译

`retrieval.go:68-76` 替换为：

```go
if len(req.DocumentIDs) > 0 {
    if len(req.DocumentIDs) > 200 {
        return nil, errorx.New(errno.ErrKnowledgeInvalidParamCode,
            errorx.KV("msg", fmt.Sprintf("DocumentIDs exceeds 200 (got %d)", len(req.DocumentIDs))))
    }
    docs, err := i.mapping.DocsByCozeIDs(ctx, req.DocumentIDs)
    if err != nil { return nil, err }
    ragDocIDs := make([]string, 0, len(docs))
    for _, d := range docs {
        ragDocIDs = append(ragDocIDs, d.RagDocID)
    }
    if len(ragDocIDs) > 0 {
        ragReq.DocumentIDs = ragDocIDs
    } else {
        // All ids unmapped (soft-deleted or drift). Logging this matters since
        // the user asked to scope retrieval and we're scoping to "nothing
        // mapped"—falling through to whole-KB search would be worse.
        logs.CtxWarnf(ctx, "ragimpl.Retrieve: all %d DocumentIDs had no mapping; returning empty hits", len(req.DocumentIDs))
        return &knowledgeModel.RetrieveResponse{}, nil
    }
}
```

## 4. Files touched

| File | Change |
|---|---|
| `backend/infra/contract/rag/types.go` | RetrieveRequest 加字段 + 更新注释 |
| `backend/infra/rag/client_test.go` | httptest 加 case lock body 含 document_ids |
| `backend/domain/knowledge/service/ragimpl/retrieval.go` | 替换 line 68-76 |
| `backend/domain/knowledge/service/ragimpl/retrieval_test.go` | unit test：translate / mapping 缺失 / 超量 / 空列表 |

## 5. Testing

Unit（`retrieval_test.go`）：
- `TestRetrieve_DocumentIDs_Translated` — `req.DocumentIDs = [1, 2]`，mapping 返回两条，fake client 收到 `ragReq.DocumentIDs = ["uuid-1", "uuid-2"]`。
- `TestRetrieve_DocumentIDs_AllUnmapped` — mapping 返回空，方法返回空 RetrieveResponse 不调 rag。
- `TestRetrieve_DocumentIDs_PartiallyMapped` — mapping 返回 1/2，fake client 收到 1 个，仍调 rag。
- `TestRetrieve_DocumentIDs_Over200` — 长度 201，返回 ErrKnowledgeInvalidParamCode，不调 mapping / rag。
- `TestRetrieve_DocumentIDs_Empty_FallsThrough` — 空切片不设置 `ragReq.DocumentIDs`。

httptest：fake rag 收到 body 含 `"document_ids": ["uuid-1"]` 字段。

## 6. Compatibility & rollout

- 完全 additive。已有调用 `req.DocumentIDs = nil` 的客户端行为不变。
- `service.Knowledge` 接口形状不变。
- 删 WARN 也是改善（噪声减少）。
- 唯一行为差异：以前 "DocumentIDs 被忽略 → 命中整库" 现在 "DocumentIDs 生效 → 命中范围收窄"。这是 bug fix，不是 breaking change。
