# R2-A: CreateDocument multipart + MinIO fetch

**Date:** 2026-05-14
**Status:** Draft
**Predecessor:** `2026-05-13-coze-ui-rag-flow-alignment-design.md` (Phase 1.5)
**Sibling slices:** R2-B (field renames), R2-C (Retrieve + ErrorBody), R2-D (new endpoints), R2-E (broader test scaffolding) — all deferred to follow-on specs

## 1. Motivation

The 2026-05-14 round-2 contract audit (against rag `0e1f49b`) revealed that rag changed its `POST /api/v1/knowledgebases/{kb_id}/documents` endpoint from JSON-with-`source_uri` to a **multipart-with-bytes** contract. Coze still sends the old JSON shape, so every rag-backed document upload returns 422 and the Phase 1.5 wizard dead-ends.

This is the single blocker preventing end-to-end smoke from passing for `KNOWLEDGE_BACKEND=rag`. The other four audit deltas (R2-B through R2-D) degrade observability and feature coverage but don't break the upload happy path; they will land in separate specs.

This document specifies the contract realignment for `CreateDocument` only, plus the ragimpl change to fetch document bytes from MinIO before calling rag.

## 2. Goals & non-goals

### Goals

- Coze's `Client.CreateDocument` produces a `multipart/form-data` body matching rag's `upload_document` route signature (`app/api/routes/documents.py:22-75`).
- Ragimpl fetches file bytes from the existing MinIO storage layer using `entity.Document.URI` as the object key.
- The wire shape is locked by a `httptest.NewServer`-based contract test in `backend/infra/rag/client_test.go`, so the next drift in this endpoint fails a unit test rather than a smoke.
- Phase 1.5's wizard flow continues to work end-to-end after this change: upload → 200 from rag → document enters processing → progress poll shows status (progress field shift is R2-B's concern).

### Non-goals

- `GetTask`, `GetDocument`, `ListDocuments` field renames (R2-B).
- `Retrieve.query_image` shape change and `ErrorBody` decoder unification (R2-C).
- Wiring rag's new endpoints `GET /capabilities`, `POST /documents/{id}/retry`, `GET /document-parameter-schemas` (R2-D).
- Extending the broader `rag-contract-check` to body schemas, or adding httptest scaffolds for the other rag endpoints (R2-E).
- Projecting coze's full `ParsingStrategy` / `TableSheet` / `CaptionType` / `FilterStrategy` surfaces into rag's `document_options` JSON — deferred to R2-D where rag's `/document-parameter-schemas` becomes the source of truth for what coze should send.
- Streaming the file body from MinIO to rag via `io.Pipe`. Rag's handler does `await file.read()`, fully materializing the bytes server-side, so streaming on coze's side gives no end-to-end memory win. Storage interface stays `[]byte`-based.

## 3. Contract change

### 3.1 Rag's multipart contract (frozen as of `0e1f49b`)

```python
@router.post("", response_model=ResponseEnvelope[UploadDocumentResponse])
async def upload_document(
    kb_id: str,
    file: UploadFile = File(...),
    file_type: str = Form(...),
    source_modality: str = Form(...),
    enable_ocr: bool = Form(False),
    enable_image_embedding: bool = Form(False),
    ocr_model_id: str | None = Form(None),
    target_chunk_types: str | None = Form(None),
    document_options: str | None = Form(None),
    chunk_size: int | None = Form(None),
    chunk_overlap: int | None = Form(None),
    tags: str | None = Form(None),
    category: str | None = Form(None),
    source_type: str | None = Form(None),
    source_id: str | None = Form(None),
    extra_metadata: str | None = Form(None),
    ...
)
```

`X-Tenant-Id` header still required (rejected with 40001 otherwise).

### 3.2 Coze-side `CreateDocumentRequest` after R2-A

| Field | Type | Required | Maps to rag form field |
|---|---|---|---|
| `FileBytes` | `[]byte` | yes | `file` (binary part) |
| `Filename` | `string` | yes | the file part's `filename=` attribute |
| `FileType` | `string` | yes | `file_type` |
| `SourceModality` | `string` | yes | `source_modality` |
| `ChunkSize` | `*int` | no | `chunk_size` |
| `ChunkOverlap` | `*int` | no | `chunk_overlap` |
| `ExtraMetadata` | `string` | no | `extra_metadata` (JSON-stringified) |

Dropped fields: `SourceURI`, `ParsingStrategy`, `ChunkingStrategy`, `Metadata` (last one is replaced by `ExtraMetadata`).

`CreateDocumentResponse` is unchanged. Rag still returns `ResponseEnvelope[UploadDocumentResponse]` with `{doc_id, task_id, status}`.

## 4. Architecture

### 4.1 Flow

```
UI upload
  → application/knowledge.CreateDocument
    → svc.ragimpl.CreateDocument               (per-doc loop, sequential)
        → storage.GetObject(d.URI) → []byte    [NEW]
        → contract.CreateDocumentRequest{
            FileBytes, Filename, FileType,
            SourceModality, ChunkSize, ChunkOverlap,
            ExtraMetadata,
          }
        → rag.Client.CreateDocument
            → doMultipart                       [NEW]
            → POST .../{kb_id}/documents (multipart/form-data)
          ← envelope.data → CreateDocumentResponse
        → idgen.GenID
        → mapping.InsertDoc
  ← entity.Document
```

### 4.2 Touched files

| File | Change |
|---|---|
| `backend/infra/contract/rag/types.go` | Rewrite `CreateDocumentRequest` per §3.2. |
| `backend/infra/rag/client.go` | Add `doMultipart`; rewrite `Client.CreateDocument` to build multipart body and call it. |
| `backend/infra/rag/client_test.go` | New file. `httptest`-based contract test for `CreateDocument`. |
| `backend/domain/knowledge/service/ragimpl/document.go` | `CreateDocument` per-doc: fetch bytes via `i.storage.GetObject`, populate new request shape, derive `FileType`/`ChunkSize`/`ChunkOverlap`/`ExtraMetadata`. |
| `backend/domain/knowledge/service/ragimpl/impl.go` (or wherever `Impl` is constructed) | Add `storage storage.Storage` field; inject in constructor. |
| Composition root that builds `ragimpl.Impl` | Pass the existing `Storage` singleton into the constructor. |

## 5. Components

### 5.1 `doMultipart` in `client.go`

A sibling to the existing `doJSON`. Same envelope/error contract — reuses `envelope` decoder and `MapRagError`. Signature:

```go
func (c *Client) doMultipart(
    ctx context.Context,
    method, path, tenantID string,
    body io.Reader,
    contentType string, // from multipart.Writer.FormDataContentType()
    out any,
    timeout time.Duration,
) error
```

Differences from `doJSON`:
- Caller provides the body reader and content type; `doMultipart` does not marshal anything itself.
- No retry: POST is non-idempotent; matches `doJSON`'s rule.

`doJSON` and `doMultipart` share a small internal helper for the response-side envelope decode if duplication is annoying. Inlining is acceptable for two callers.

### 5.2 Multipart builder in `Client.CreateDocument`

```go
func (c *Client) CreateDocument(ctx context.Context, tenantID, kbID string, req *contract.CreateDocumentRequest) (*contract.CreateDocumentResponse, error) {
    var buf bytes.Buffer
    w := multipart.NewWriter(&buf)
    // file part — must use CreateFormFile so the filename attribute is set
    fw, err := w.CreateFormFile("file", req.Filename)
    if err != nil { ... }
    if _, err := fw.Write(req.FileBytes); err != nil { ... }
    // required form fields
    _ = w.WriteField("file_type", req.FileType)
    _ = w.WriteField("source_modality", req.SourceModality)
    // optional form fields
    if req.ChunkSize != nil { _ = w.WriteField("chunk_size", strconv.Itoa(*req.ChunkSize)) }
    if req.ChunkOverlap != nil { _ = w.WriteField("chunk_overlap", strconv.Itoa(*req.ChunkOverlap)) }
    if req.ExtraMetadata != "" { _ = w.WriteField("extra_metadata", req.ExtraMetadata) }
    if err := w.Close(); err != nil { ... }

    out := &contract.CreateDocumentResponse{}
    path := apiPrefix + "/knowledgebases/" + kbID + "/documents"
    timeout := time.Duration(c.cfg.UploadTimeoutMs) * time.Millisecond
    if err := c.doMultipart(ctx, http.MethodPost, path, tenantID, &buf, w.FormDataContentType(), out, timeout); err != nil {
        return nil, err
    }
    return out, nil
}
```

Inlined deliberately: the field set is fixed, and putting it behind a builder type would obscure the wire shape, which is exactly what R2-A is trying to make explicit.

### 5.3 Ragimpl `CreateDocument`

```go
for _, d := range req.Documents {
    m, err := i.mapping.KBByCozeID(ctx, d.KnowledgeID)
    if err != nil { return nil, err }

    fileBytes, err := i.storage.GetObject(ctx, d.URI)
    if err != nil { return nil, err }

    var chunkSize, chunkOverlap *int
    if d.ChunkingStrategy != nil {
        if d.ChunkingStrategy.ChunkSize > 0 {
            s := int(d.ChunkingStrategy.ChunkSize)
            chunkSize = &s
        }
        if d.ChunkingStrategy.Overlap > 0 {
            o := int(d.ChunkingStrategy.Overlap)
            chunkOverlap = &o
        }
    }

    extraMetadata, _ := json.Marshal(buildDocMetadata(d))

    ragReq := &contract.CreateDocumentRequest{
        FileBytes:      fileBytes,
        Filename:       d.Name,
        FileType:       string(d.FileExtension),
        SourceModality: sourceModalityFor(d),
        ChunkSize:      chunkSize,
        ChunkOverlap:   chunkOverlap,
        ExtraMetadata:  string(extraMetadata),
    }
    ragResp, err := i.rag.CreateDocument(ctx, tenant, m.RagKBID, ragReq)
    // … existing idgen + mapping + rollback unchanged …
}
```

The exact field names on `ChunkingStrategy` (e.g. `ChunkSize` vs `MaxTokens`, `Overlap` vs `OverlapSize`) are read off the entity during implementation; the spec assumes only that *some* size/overlap values exist there and are mapped one-to-one.

### 5.4 Storage dependency wiring

`ragimpl.Impl` currently does not hold a `storage.Storage`. Add it as a constructor parameter; the composition root that builds `Impl` already has the `Storage` singleton available (it's wired into every other domain that touches MinIO).

If `Impl`'s constructor signature grows uncomfortably, the existing options-pattern (if present) or a small `Deps` struct refactor is acceptable. This is a scope decision deferred to implementation — both shapes are equally fine for R2-A.

## 6. Data flow & invariants

- **Document.URI is the MinIO object key.** This is the same key the legacy implementation has used since Phase 1; no migration needed.
- **No rag-side rollback on MinIO failure.** Bytes are fetched before any rag call; if fetch fails, return the error and let the caller retry.
- **rag-side rollback on idgen/InsertDoc failure** stays as today: best-effort `DeleteDocument` against rag, logged on failure. Spec §5.3 preserves this path.
- **Sequential per-doc loop** stays. R2-A does not change concurrency.
- **No file-size cap on the coze side.** If rag rejects a large file, surface the error unchanged.

## 7. Error handling

| Failure | Behavior |
|---|---|
| `storage.GetObject` returns error | Return error to caller; no rag call made; nothing to roll back. |
| `rag.CreateDocument` returns 4xx/5xx | Existing `MapRagError` path. Pydantic 422 from rag (e.g. missing required form field) classifies via the current `ErrorBody` decoder; R2-C will improve this but R2-A is content with current behavior (error string still preserved, just maps to `upstream-unavailable`). |
| `idgen.GenID` or `mapping.InsertDoc` fail after rag accepts | Existing rollback path: best-effort `DeleteDocument(rag)`, logged. |

## 8. Testing

### 8.1 New contract test — `backend/infra/rag/client_test.go::TestCreateDocument_Multipart`

`httptest.NewServer` with a handler that asserts:

1. Method is `POST`; path matches `/api/v1/knowledgebases/{kb_id}/documents`.
2. `X-Tenant-Id` header is set.
3. `Content-Type` starts with `multipart/form-data; boundary=`.
4. `multipart.NewReader` parses the body and yields parts with the expected names: `file`, `file_type`, `source_modality`, optionally `chunk_size`, `chunk_overlap`, `extra_metadata`.
5. The `file` part's `filename=` attribute matches the request, and the bytes round-trip exactly.
6. The handler returns a `ResponseEnvelope` with `{doc_id: "d1", task_id: "t1", status: "pending"}`; the test asserts the decoded `CreateDocumentResponse` matches.

Failure path: a second sub-test has the handler return HTTP 422 with the current FastAPI `{"detail": {"code": 40001, "message": "..."}}` envelope; assert `CreateDocument` returns a non-nil error whose message contains the rag message. (We are not pinning the error *classification* here — that's an R2-C deliverable.)

### 8.2 Existing tests

- `backend/domain/knowledge/service/ragimpl/integration_test.go` stubs the rag client. The stub's `CreateDocument` signature is unchanged (request type now has different fields, but the method shape is identical), so no test code in this file should need to change. Verify during impl.
- `make middleware && GOTOOLCHAIN=go1.24.0 go test ./infra/contract/rag/... ./infra/rag/... ./domain/knowledge/service/ragimpl/... ./application/knowledge/...` stays green.

### 8.3 Smoke validation

Manual smoke after impl:
1. Bring up rag + coze middleware per the recipe in the project memory snapshot (item #2).
2. Log in, create a rag-backed KB (text mode), upload a small `.txt` or `.pdf`.
3. Expect: HTTP 200 from `POST .../documents`; KB detail polling shows the doc moving through `pending → processing`; eventually `ready` (or `failed` with a real reason — not the current 422-on-create wall).

## 9. Compatibility & rollout

- Feature-flagged by the existing `KNOWLEDGE_BACKEND` env var; legacy backend path is untouched.
- No DB migration. The rag `rag_doc_mapping` table fields remain identical.
- No frontend change. Upload wizard already sends the right fields; this slice only fixes how coze forwards them.

## 10. Open questions

None that block writing the implementation plan. Two minor items resolved during impl:

1. Exact field names on `entity.ChunkingStrategy` (verified once during impl; spec §5.3 placeholders stand in until then).
2. Whether `Impl`'s constructor uses a positional `storage` parameter or a `Deps` struct refactor (cosmetic; either works).
