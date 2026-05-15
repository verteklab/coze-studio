# R2-H: wire `UpdateDocument` to rag's `POST /documents/{doc_id}/update`

**Date:** 2026-05-15
**Status:** Draft
**Predecessor:** R2-F (2026-05-14)
**Sibling slices:** R2-I / R2-J / R2-K / R2-L / R2-F-Rerank / R2-G —— 同期可并行
**Companion gap:** `docs/rag-feature-gaps-zh.md` §A' 行 "文档元数据更新"

## 1. Motivation

`backend/domain/knowledge/service/ragimpl/unsupported.go:44-46` 把 `UpdateDocument` stub 出去返回 `105100001`。KB 详情页 → 文档 → "改文档名 / 标签 / 自定义字段" 入口全部失败。

Rag 端已上线 `POST /knowledgebases/{kb_id}/documents/{doc_id}/update`（`rag/app/api/routes/documents.py:117`），接受 `filename / tags / category / source_type / source_id / extra_metadata`，返回完整的 `DocumentDetail`。R2-H 把 ragimpl 的 stub 换成真实调用。

## 2. Goals & non-goals

### Goals

- `backend/infra/contract/rag/client.go` 加 `UpdateDocument(ctx, tenantID, kbID, docID string, req *UpdateDocumentRequest) (*Document, error)`。
- `backend/infra/contract/rag/types.go` 加 `UpdateDocumentRequest` DTO（`Filename *string` / `Tags *[]string` / `Category *string` / `SourceType *string` / `SourceID *string` / `ExtraMetadata map[string]any`；指针类型用来区分 unset vs 空值，匹配 rag pydantic 的 `exclude_unset`）。
- `backend/infra/rag/client.go` 实现 HTTP 调用，端点是 `POST /api/v1/knowledgebases/{kb_id}/documents/{doc_id}/update`，response 解 `Document` DTO。
- `backend/domain/knowledge/service/ragimpl/document.go` 加 `UpdateDocument` 真实实现：通过 `mapping.DocByCozeID(req.DocumentID)` 翻译到 rag id，把 `req.DocumentName` → `Filename`，调 Client，丢弃 response 返回 nil error（service interface 返回 `error` 而非 `*Document`）。
- `backend/domain/knowledge/service/ragimpl/unsupported.go:44-46` 删 stub。
- `req.TableInfo` 非 nil 时返回 `ErrKnowledgeInvalidParam` "table metadata update pending table ingestion"（表格摄入未实现，rag 端 update DTO 也不接受 table 字段）。
- Unit test：happy path（仅 DocumentName）+ TableInfo 拒绝 + mapping 缺失返回 ErrKnowledgeNotFound。
- httptest：锁形 POST `/documents/{doc_id}/update` 请求 body 与 response 解析。

### Non-goals

- 暴露更丰富的元数据字段到 `service.UpdateDocumentRequest`（目前只有 `DocumentName` 和 `TableInfo`）。如要支持 tags / category 等，需独立 slice 改 service interface + IDL + UI；本 slice 只解决"改文档名"基础能力。
- 表格相关字段的 update（属 bucket A 的表格摄入项）。
- 前端改动。UI 已经在调 service.UpdateDocument，本 slice 修通后端即可。

## 3. Contract change

### 3.1 New DTO

`backend/infra/contract/rag/types.go`：

```go
type UpdateDocumentRequest struct {
    Filename      *string         `json:"filename,omitempty"`
    Tags          *[]string       `json:"tags,omitempty"`
    Category      *string         `json:"category,omitempty"`
    SourceType    *string         `json:"source_type,omitempty"`
    SourceID      *string         `json:"source_id,omitempty"`
    ExtraMetadata map[string]any  `json:"extra_metadata,omitempty"`
}
```

指针 + `omitempty` 让 rag 端 `model_dump(exclude_unset=True)` 看到的就是 coze 实际想更新的字段；coze 未指定 = nil = JSON 字段缺失 = rag 不动该字段。

### 3.2 Client method

```go
func (c *Client) UpdateDocument(ctx context.Context, tenantID, kbID, docID string,
    req *UpdateDocumentRequest) (*Document, error)
```

走 `doJSON(POST, "/api/v1/knowledgebases/{kb_id}/documents/{doc_id}/update", req, &Document{})`，header 走 `X-Tenant-Id: tenantID`。

### 3.3 ragimpl.UpdateDocument

```go
func (i *Impl) UpdateDocument(ctx context.Context, req *service.UpdateDocumentRequest) error {
    if req.TableInfo != nil {
        return errorx.New(errno.ErrKnowledgeInvalidParamCode,
            errorx.KV("msg", "table metadata update pending table ingestion support"))
    }
    tenant, err := i.tenant(ctx)
    if err != nil { return err }
    m, err := i.mapping.DocByCozeID(ctx, req.DocumentID)
    if err != nil { return err }
    payload := &contract.UpdateDocumentRequest{}
    if req.DocumentName != nil { payload.Filename = req.DocumentName }
    _, err = i.rag.UpdateDocument(ctx, tenant, m.RagKBID, m.RagDocID, payload)
    return err
}
```

## 4. Files touched

| File | Change |
|---|---|
| `backend/infra/contract/rag/types.go` | 加 `UpdateDocumentRequest` |
| `backend/infra/contract/rag/client.go` | 接口里加 `UpdateDocument` 方法 |
| `backend/infra/rag/client.go` | 实现 |
| `backend/infra/rag/client_test.go` | httptest 锁形 |
| `backend/domain/knowledge/service/ragimpl/document.go` | 加真实 `UpdateDocument` 实现 |
| `backend/domain/knowledge/service/ragimpl/unsupported.go` | 删 stub |
| `backend/domain/knowledge/service/ragimpl/document_test.go` | unit test（happy / TableInfo reject / mapping not found） |
| `backend/domain/knowledge/service/ragimpl/unsupported_test.go` | 删对应 stub 期望 |
| `backend/internal/mock/infra/rag/client_mock.go` | mockgen 重新生成 |

## 5. Testing

Unit：
- `TestUpdateDocument_HappyPath_RenameOnly` — fake client 收到 `Filename: ptr("new.pdf")`，mapping 翻译正确，返回 nil error。
- `TestUpdateDocument_TableInfo_Rejected` — `req.TableInfo != nil` 直接返回 `ErrKnowledgeInvalidParamCode`，不调 rag。
- `TestUpdateDocument_MappingNotFound` — `DocByCozeID` 返回 not-found，方法透传错误，不调 rag。
- `TestUpdateDocument_RagError_Propagated` — fake client 返回 40404，方法透传。

httptest：lock POST body 为 `{"filename":"new.pdf"}`（注意 unset 字段不出现）、tenant header、response 解 `Document`。

## 6. Compatibility & rollout

完全 additive：未实现前 service stub 报错；实现后旧调用方语义不变，仅"以前会报错的调用现在成功"。无 DB 迁移、无 IDL 改、无前端改。
