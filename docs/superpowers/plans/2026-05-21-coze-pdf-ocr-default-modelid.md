# Coze PDF Upload: Attach Default `ocr_model_id` — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** For every PDF upload that does not already carry an `ocr_model_id` in `document_options`, coze backend attaches the env-driven `RAG_DEFAULT_OCR_MODEL_ID` to the multipart top-level `ocr_model_id` field. After rag's auto-promote from `text_source` to `scanned_document_source` (on image-content PDFs), the scanned-schema validator's `ocr_model_id is required when enable_ocr is true` check passes instead of 400-ing.

**Architecture:** Mirrors the existing pattern set by R2-F (`DefaultLLMModelID`) and R2-F-Rerank (`DefaultRerankModelID`). New `Config.DefaultOCRModelID` flows from `rag.yaml` → `ragimpl.Impl.defaultOCRModelID` → `CreateDocumentRequest.OcrModelID` → multipart field. The injection site (`ragimpl.CreateDocument`) gates on `d.FileExtension == parser.FileExtensionPDF` AND `documentOptions` not already carrying the key — so existing scanned-PDF uploads (form supplies `ocr_model_id` in `document_options`) are untouched and rag's `flat_options.get("ocr_model_id")` fallback continues to do its job.

**Tech Stack:** Go 1.24, `gopkg.in/yaml.v3`, `mime/multipart`, `encoding/json`, standard `testing`. Backend-only — no frontend, no rag-server changes.

**Files Touched:**
- Modify: `backend/conf/rag/config.go` — add `DefaultOCRModelID` field
- Modify: `backend/conf/rag/rag.yaml` — add `default_ocr_model_id` entry
- Modify: `backend/conf/rag/config_test.go` — extend env-substitution test
- Modify: `backend/domain/knowledge/service/ragimpl/factory.go` — add `defaultOCRModelID` field + extend `New(...)` signature
- Modify: `backend/application/knowledge/init.go` — pass `cfg.Rag.DefaultOCRModelID` to `ragimpl.New(...)`
- Modify: `backend/domain/knowledge/service/ragimpl/knowledge_test.go` — extend `newTestImpl` helper
- Modify: `backend/infra/contract/rag/types.go` — add `OcrModelID *string` to `CreateDocumentRequest`
- Modify: `backend/infra/rag/client.go` — write `ocr_model_id` multipart field when non-nil
- Modify: `backend/infra/rag/client_test.go` — extend multipart test (already exists per the r2a plan)
- Modify: `backend/domain/knowledge/service/ragimpl/document.go` — add `documentOptionsHasOCRModelID` helper + wire injection in `CreateDocument`
- Modify: `backend/domain/knowledge/service/ragimpl/document_test.go` — add OCR-attach cases
- Modify: `docker/.env.v2` — add `RAG_DEFAULT_OCR_MODEL_ID`
- Modify: `docker/.env.debug` — same
- Modify: `docker/.env.debug.example` — same

**Test command (run from repo root):** `cd backend && GOTOOLCHAIN=go1.24.0 go test ./conf/rag/... ./infra/contract/rag/... ./infra/rag/... ./domain/knowledge/service/ragimpl/... ./application/knowledge/...`

---

## Task 1: Add `DefaultOCRModelID` to config

Add the YAML-bound config field and its `${RAG_DEFAULT_OCR_MODEL_ID}` placeholder. Pure data, no behavior yet — but extending the env-substitution test keeps the loader honest.

**Files:**
- Modify: `backend/conf/rag/config.go`
- Modify: `backend/conf/rag/rag.yaml`
- Modify: `backend/conf/rag/config_test.go`

---

- [ ] **Step 1: Extend the failing config test**

Open `backend/conf/rag/config_test.go`. Find the test that asserts `cfg.Rag.DefaultLLMModelID` round-trips (search for `DefaultLLMModelID`). Add the same assertion shape for the new field. Concretely, in the YAML fixture used by that test:

- The fixture string includes a line like `default_llm_model_id: "${RAG_DEFAULT_LLM_MODEL_ID}"` — add an analogous line:

```yaml
default_ocr_model_id: "${RAG_DEFAULT_OCR_MODEL_ID}"
```

- In the test body, where `t.Setenv("RAG_DEFAULT_LLM_MODEL_ID", ...)` is called, also call:

```go
t.Setenv("RAG_DEFAULT_OCR_MODEL_ID", "model-ocr-paddle-infer-text")
```

- In the assertions block, where `assert.Equal(t, "...", cfg.Rag.DefaultLLMModelID)` lives, add:

```go
assert.Equal(t, "model-ocr-paddle-infer-text", cfg.Rag.DefaultOCRModelID)
```

If the existing test file does not yet have a single round-trip test that covers all default model id fields, add a fresh test:

```go
func TestLoad_DefaultOCRModelID_FromEnv(t *testing.T) {
    t.Setenv("RAG_DEFAULT_OCR_MODEL_ID", "model-ocr-paddle-infer-text")
    dir := t.TempDir()
    path := filepath.Join(dir, "rag.yaml")
    require.NoError(t, os.WriteFile(path, []byte(`
rag:
  base_url: "http://rag:8000"
  timeout_ms: 1000
  default_text_embedding_model_id: "x"
  default_image_embedding_model_id: "y"
  default_llm_model_id: ""
  default_rerank_model_id: ""
  default_ocr_model_id: "${RAG_DEFAULT_OCR_MODEL_ID}"
knowledge:
  backend: "rag"
  tenant:
    mode: "env"
    default_tenant_id: "test"
`), 0o600))
    cfg, err := Load(path)
    require.NoError(t, err)
    assert.Equal(t, "model-ocr-paddle-infer-text", cfg.Rag.DefaultOCRModelID)
}
```

(Add `"path/filepath"`, `"os"`, `"github.com/stretchr/testify/assert"`, `"github.com/stretchr/testify/require"` to the import block if not already present.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./conf/rag/... -run "TestLoad_DefaultOCRModelID_FromEnv" -v`
Expected: FAIL — `cfg.Rag.DefaultOCRModelID undefined`.

- [ ] **Step 3: Add `DefaultOCRModelID` to `Config`**

Open `backend/conf/rag/config.go`. After the existing `DefaultRerankModelID string` field in `Config`, add:

```go
// DefaultOCRModelID is the OCR model id coze attaches as the multipart
// top-level `ocr_model_id` for PDF uploads when the dynamic form did not
// supply one. rag's PDF auto-detector (services/document_service.py
// inspect_pdf_source_modality) silently promotes no-text-layer PDFs from
// text_source to scanned_document_source, after which the scanned-schema
// validator requires `ocr_model_id`. Empty value disables the attachment.
DefaultOCRModelID string `yaml:"default_ocr_model_id"`
```

- [ ] **Step 4: Add the YAML entry**

Open `backend/conf/rag/rag.yaml`. After the `default_rerank_model_id: ...` line, add:

```yaml
  # OCR model id attached to multipart top-level `ocr_model_id` for PDF
  # uploads when the upload form did not supply one. Required because rag's
  # PDF auto-detector silently promotes no-text-layer PDFs to
  # scanned_document_source, after which the validator requires this field.
  default_ocr_model_id: "${RAG_DEFAULT_OCR_MODEL_ID}"
```

Also open `bin/resources/conf/rag/rag.yaml` (the runtime copy) and make the same addition. The build script copies from `backend/conf/rag/rag.yaml` over `bin/resources/conf/rag/rag.yaml` on `make server`, but the v2 stack image bakes the file in — so updating both keeps the spec aligned and the next image rebuild picks it up.

- [ ] **Step 5: Run test to verify it passes**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./conf/rag/... -v`
Expected: PASS (all existing tests + the new one).

- [ ] **Step 6: Do not commit yet.** Continues into Task 2.

---

## Task 2: Extend `ragimpl.Impl` + `New(...)` signature

Add the field on the impl struct and a constructor parameter for it. This step intentionally breaks the build at `application/knowledge/init.go` and at `newTestImpl`; the next two tasks fix those.

**Files:**
- Modify: `backend/domain/knowledge/service/ragimpl/factory.go`

---

- [ ] **Step 1: Add field to `Impl` struct**

Open `backend/domain/knowledge/service/ragimpl/factory.go`. After the `defaultRerankModelID string` field, add:

```go
// defaultOCRModelID is attached to multipart top-level `ocr_model_id` for
// PDF uploads when the dynamic form did not supply one. Required because
// rag's PDF auto-detector silently promotes no-text-layer PDFs to
// scanned_document_source; the scanned-schema validator then rejects the
// request without ocr_model_id. Empty value disables the attachment.
defaultOCRModelID string
```

- [ ] **Step 2: Extend `New(...)` signature and body**

Change the `New(...)` signature in the same file from:

```go
func New(
    rag contract.Client,
    db *gorm.DB,
    idgen idgen.IDGenerator,
    resolver TenantResolver,
    storage storage.Storage,
    defaultTextModel, defaultImageModel, defaultLLMModel, defaultRerankModel string,
) *Impl {
    return &Impl{
        rag:                          rag,
        mapping:                      NewMappingRepo(db),
        idgen:                        idgen,
        resolver:                     resolver,
        storage:                      storage,
        defaultTextEmbeddingModelID:  defaultTextModel,
        defaultImageEmbeddingModelID: defaultImageModel,
        defaultLLMModelID:            defaultLLMModel,
        defaultRerankModelID:         defaultRerankModel,
    }
}
```

to:

```go
func New(
    rag contract.Client,
    db *gorm.DB,
    idgen idgen.IDGenerator,
    resolver TenantResolver,
    storage storage.Storage,
    defaultTextModel, defaultImageModel, defaultLLMModel, defaultRerankModel, defaultOCRModel string,
) *Impl {
    return &Impl{
        rag:                          rag,
        mapping:                      NewMappingRepo(db),
        idgen:                        idgen,
        resolver:                     resolver,
        storage:                      storage,
        defaultTextEmbeddingModelID:  defaultTextModel,
        defaultImageEmbeddingModelID: defaultImageModel,
        defaultLLMModelID:            defaultLLMModel,
        defaultRerankModelID:         defaultRerankModel,
        defaultOCRModelID:            defaultOCRModel,
    }
}
```

- [ ] **Step 3: Verify the package no longer builds**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./application/knowledge/... ./domain/knowledge/service/ragimpl/...`
Expected: FAIL at `application/knowledge/init.go` with "not enough arguments in call to ragimpl.New" (the 8-arg call site, fixed in Task 3) and at `domain/knowledge/service/ragimpl/knowledge_test.go` with "unknown field defaultOCRModelID" being absent (also fixed in Task 3 if you choose; Step 4 below makes the helper update mandatory).

- [ ] **Step 4: Do not commit yet.** Continues into Task 3.

---

## Task 3: Fix call sites — `init.go` and `newTestImpl`

**Files:**
- Modify: `backend/application/knowledge/init.go`
- Modify: `backend/domain/knowledge/service/ragimpl/knowledge_test.go`

---

- [ ] **Step 1: Update `init.go` to pass the new param**

Open `backend/application/knowledge/init.go`. Find the `ragimpl.New(...)` call (around line 98). Change the arg block from:

```go
domainSVC := ragimpl.New(
    client,
    c.DB,
    c.IDGen,
    resolver,
    c.Storage,
    cfg.Rag.DefaultTextEmbeddingModelID,
    cfg.Rag.DefaultImageEmbeddingModelID,
    cfg.Rag.DefaultLLMModelID,
    cfg.Rag.DefaultRerankModelID,
)
```

to:

```go
domainSVC := ragimpl.New(
    client,
    c.DB,
    c.IDGen,
    resolver,
    c.Storage,
    cfg.Rag.DefaultTextEmbeddingModelID,
    cfg.Rag.DefaultImageEmbeddingModelID,
    cfg.Rag.DefaultLLMModelID,
    cfg.Rag.DefaultRerankModelID,
    cfg.Rag.DefaultOCRModelID,
)
```

- [ ] **Step 2: Update `newTestImpl` helper**

Open `backend/domain/knowledge/service/ragimpl/knowledge_test.go`. Find `func newTestImpl(...)` (around line 342). Add `defaultOCRModelID` to the struct literal — after `defaultRerankModelID: "rerank-model-default",`:

```go
defaultOCRModelID:            "ocr-model-default",
```

The final literal block should look like:

```go
return &Impl{
    rag:                          fc,
    mapping:                      NewMappingRepo(db),
    idgen:                        &stubIDGen{ids: ids},
    resolver:                     NewEnvTenantResolver("test-tenant"),
    storage:                      &stubStorage{},
    defaultTextEmbeddingModelID:  "text-model-default",
    defaultImageEmbeddingModelID: "image-model-default",
    defaultLLMModelID:            "llm-model-default",
    defaultRerankModelID:         "rerank-model-default",
    defaultOCRModelID:            "ocr-model-default",
}
```

- [ ] **Step 3: Verify build passes**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./application/knowledge/... ./domain/knowledge/service/ragimpl/... ./conf/rag/...`
Expected: PASS (no output).

- [ ] **Step 4: Verify existing tests still green**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./domain/knowledge/service/ragimpl/... ./application/knowledge/... ./conf/rag/...`
Expected: PASS for all existing tests.

- [ ] **Step 5: Commit progress so far**

```bash
git add backend/conf/rag/config.go backend/conf/rag/rag.yaml backend/conf/rag/config_test.go \
        backend/domain/knowledge/service/ragimpl/factory.go \
        backend/domain/knowledge/service/ragimpl/knowledge_test.go \
        backend/application/knowledge/init.go \
        bin/resources/conf/rag/rag.yaml
git commit -m "feat(knowledge-rag): thread DefaultOCRModelID through config → ragimpl"
```

---

## Task 4: Add wire field `OcrModelID` + multipart write

Field is optional, pointer-shaped, mirrors the existing `EnableOCR *bool` style. nil means "do not write the multipart field" so rag's `flat_options.get("ocr_model_id")` fallback continues to work for existing scanned uploads.

**Files:**
- Modify: `backend/infra/contract/rag/types.go`
- Modify: `backend/infra/rag/client.go`
- Modify: `backend/infra/rag/client_test.go`

---

- [ ] **Step 1: Find or create the multipart-shape test**

Run: `cd backend && grep -n "WriteField.*enable_ocr\|CreateDocument" infra/rag/client_test.go | head -10`

If a test asserts on the multipart shape of `CreateDocument` exists (likely `TestCreateDocument_MultipartShape` or similar from r2a), extend it. If not, create one. The pattern is to call `client.CreateDocument(...)` against an `httptest.NewServer` that captures the multipart body, then assert on the form fields.

- [ ] **Step 2: Write failing test for the new field**

Append to `backend/infra/rag/client_test.go`:

```go
func TestCreateDocument_OcrModelID_WrittenWhenNonNil(t *testing.T) {
    var capturedForm map[string]string
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        require.NoError(t, r.ParseMultipartForm(1<<20))
        capturedForm = map[string]string{}
        for k, v := range r.MultipartForm.Value {
            if len(v) > 0 {
                capturedForm[k] = v[0]
            }
        }
        w.Header().Set("Content-Type", "application/json")
        _, _ = w.Write([]byte(`{"data":{"doc_id":"d1","task_id":"t1","status":"pending"}}`))
    }))
    defer srv.Close()

    c := New(ragconf.Config{BaseURL: srv.URL, TimeoutMs: 5000})
    ocrID := "model-ocr-paddle-infer-text"
    _, err := c.CreateDocument(context.Background(), "tenant-a", "kb-1", &contract.CreateDocumentRequest{
        FileBytes:      []byte("%PDF-1.4\nfake"),
        Filename:       "x.pdf",
        FileType:       "pdf",
        SourceModality: "text_source",
        OcrModelID:     &ocrID,
    })
    require.NoError(t, err)
    assert.Equal(t, "model-ocr-paddle-infer-text", capturedForm["ocr_model_id"])
}

func TestCreateDocument_OcrModelID_OmittedWhenNil(t *testing.T) {
    var seenKeys []string
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        require.NoError(t, r.ParseMultipartForm(1<<20))
        for k := range r.MultipartForm.Value {
            seenKeys = append(seenKeys, k)
        }
        w.Header().Set("Content-Type", "application/json")
        _, _ = w.Write([]byte(`{"data":{"doc_id":"d1","task_id":"t1","status":"pending"}}`))
    }))
    defer srv.Close()

    c := New(ragconf.Config{BaseURL: srv.URL, TimeoutMs: 5000})
    _, err := c.CreateDocument(context.Background(), "tenant-a", "kb-1", &contract.CreateDocumentRequest{
        FileBytes:      []byte("%PDF-1.4\nfake"),
        Filename:       "x.pdf",
        FileType:       "pdf",
        SourceModality: "text_source",
        OcrModelID:     nil,
    })
    require.NoError(t, err)
    assert.NotContains(t, seenKeys, "ocr_model_id")
}
```

If the imports `"net/http"`, `"net/http/httptest"`, `"context"`, `ragconf "github.com/coze-dev/coze-studio/backend/conf/rag"`, `contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"`, `"github.com/stretchr/testify/assert"`, `"github.com/stretchr/testify/require"` are not already present, add them.

- [ ] **Step 3: Run test to verify it fails**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./infra/rag/... -run "TestCreateDocument_OcrModelID" -v`
Expected: FAIL — `unknown field OcrModelID in struct literal of type rag.CreateDocumentRequest`.

- [ ] **Step 4: Add `OcrModelID` to `CreateDocumentRequest`**

Open `backend/infra/contract/rag/types.go`. Find `CreateDocumentRequest`. After the existing `EnableImageEmbedding *bool` field (or wherever `EnableOCR` lives — keep the file's existing field ordering), add:

```go
// OcrModelID: optional. nil means "do not write the multipart field" —
// rag will fall back to flat_options.get("ocr_model_id") from
// document_options if present. Coze sets this for PDF uploads where
// document_options does not already carry the key, so rag's validator
// passes after its silent auto-promote from text_source to
// scanned_document_source.
OcrModelID *string
```

- [ ] **Step 5: Write the multipart field**

Open `backend/infra/rag/client.go`. Find the block that writes `enable_ocr` (search for `WriteField("enable_ocr"`). Immediately after that block (preserving the file's logical grouping), add:

```go
if req.OcrModelID != nil {
    if err := w.WriteField("ocr_model_id", *req.OcrModelID); err != nil {
        return nil, fmt.Errorf("multipart write ocr_model_id: %w", err)
    }
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./infra/rag/... -run "TestCreateDocument_OcrModelID" -v`
Expected: PASS (both new tests).

Also run the full client test sweep:

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./infra/contract/rag/... ./infra/rag/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/infra/contract/rag/types.go backend/infra/rag/client.go backend/infra/rag/client_test.go
git commit -m "feat(knowledge-rag): add OcrModelID to CreateDocumentRequest multipart"
```

---

## Task 5: Domain helper — `documentOptionsHasOCRModelID`

Inspects the cleaned `document_options` JSON string to decide whether the upload form already supplied an `ocr_model_id`. If so, we leave injection alone (form precedence).

**Files:**
- Modify: `backend/domain/knowledge/service/ragimpl/document.go`
- Modify: `backend/domain/knowledge/service/ragimpl/document_test.go`

---

- [ ] **Step 1: Write failing tests**

Append to `backend/domain/knowledge/service/ragimpl/document_test.go`:

```go
func TestDocumentOptionsHasOCRModelID(t *testing.T) {
    t.Parallel()
    cases := []struct {
        name string
        raw  string
        want bool
    }{
        {"empty string", "", false},
        {"empty object", `{}`, false},
        {"unrelated keys only", `{"chunk_size": 800}`, false},
        {"key present non-empty", `{"ocr_model_id": "model-x"}`, true},
        {"key present empty string", `{"ocr_model_id": ""}`, false},
        {"key present whitespace only", `{"ocr_model_id": "   "}`, false},
        {"key present wrong type", `{"ocr_model_id": 42}`, false},
        {"malformed json", `{not json`, false},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            got := documentOptionsHasOCRModelID(tc.raw)
            assert.Equal(t, tc.want, got)
        })
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./domain/knowledge/service/ragimpl/... -run "TestDocumentOptionsHasOCRModelID" -v`
Expected: FAIL — `undefined: documentOptionsHasOCRModelID`.

- [ ] **Step 3: Add the helper**

Open `backend/domain/knowledge/service/ragimpl/document.go`. Find `applyDocumentOptionsOverrides` (around line 55). Immediately after it, add:

```go
// documentOptionsHasOCRModelID reports whether a non-empty `ocr_model_id`
// string key is present in the JSON object. Empty options, parse failure,
// non-string value, and whitespace-only values all return false. Used by
// CreateDocument to decide whether to inject the env-driven OCR model id
// default at the multipart top level — if the upload form already passed
// one inside document_options, rag's flat_options fallback handles it and
// we leave the top level empty so the form value wins.
func documentOptionsHasOCRModelID(rawJSON string) bool {
    if rawJSON == "" {
        return false
    }
    var m map[string]any
    if err := json.Unmarshal([]byte(rawJSON), &m); err != nil {
        return false
    }
    v, ok := m["ocr_model_id"]
    if !ok {
        return false
    }
    s, ok := v.(string)
    return ok && strings.TrimSpace(s) != ""
}
```

If `"encoding/json"` and `"strings"` are not already in the import block, add them.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./domain/knowledge/service/ragimpl/... -run "TestDocumentOptionsHasOCRModelID" -v`
Expected: PASS.

- [ ] **Step 5: Do not commit yet.** Continues into Task 6.

---

## Task 6: Wire injection in `CreateDocument`

Plug the new field into the rag request. Add focused tests covering the four scenarios from the spec.

**Files:**
- Modify: `backend/domain/knowledge/service/ragimpl/document.go`
- Modify: `backend/domain/knowledge/service/ragimpl/document_test.go`

---

- [ ] **Step 1: Write failing scenario tests**

Append to `backend/domain/knowledge/service/ragimpl/document_test.go`:

```go
func TestCreateDocument_OCRDefault_PDFNoFormOCR_AttachesEnvDefault(t *testing.T) {
    fc := &fakeClient{
        nextKBID:   "kb-uuid",
        nextDocID:  "doc-uuid",
        nextTaskID: "task-uuid",
    }
    i := newTestImpl(t, fc, 8801)
    seedKBMapping(t, i.mapping, 7000, "kb-uuid")

    _, err := i.CreateDocument(context.Background(), &service.CreateDocumentRequest{
        Documents: []*entity.Document{{
            Info:          knowledgeModel.Info{Name: "windowSticker.pdf"},
            KnowledgeID:   7000,
            FileExtension: parser.FileExtensionPDF,
            URI:           "any",
        }},
    })
    require.NoError(t, err)
    require.NotNil(t, fc.lastCreateDocReq, "fakeClient must capture last CreateDocument request")
    require.NotNil(t, fc.lastCreateDocReq.OcrModelID)
    assert.Equal(t, "ocr-model-default", *fc.lastCreateDocReq.OcrModelID)
}

func TestCreateDocument_OCRDefault_PDFFormSuppliedOCR_NoTopLevelAttach(t *testing.T) {
    fc := &fakeClient{
        nextKBID:   "kb-uuid",
        nextDocID:  "doc-uuid",
        nextTaskID: "task-uuid",
    }
    i := newTestImpl(t, fc, 8802)
    seedKBMapping(t, i.mapping, 7000, "kb-uuid")

    _, err := i.CreateDocument(context.Background(), &service.CreateDocumentRequest{
        Documents: []*entity.Document{{
            Info:          knowledgeModel.Info{Name: "doc.pdf"},
            KnowledgeID:   7000,
            FileExtension: parser.FileExtensionPDF,
            URI:           "any",
        }},
        DocumentOptions: `{"ocr_model_id":"user-picked-model"}`,
    })
    require.NoError(t, err)
    require.NotNil(t, fc.lastCreateDocReq)
    assert.Nil(t, fc.lastCreateDocReq.OcrModelID,
        "form-supplied ocr_model_id in document_options must take precedence; top-level stays nil")
}

func TestCreateDocument_OCRDefault_NonPDF_NoAttach(t *testing.T) {
    fc := &fakeClient{
        nextKBID:   "kb-uuid",
        nextDocID:  "doc-uuid",
        nextTaskID: "task-uuid",
    }
    i := newTestImpl(t, fc, 8803)
    seedKBMapping(t, i.mapping, 7000, "kb-uuid")

    _, err := i.CreateDocument(context.Background(), &service.CreateDocumentRequest{
        Documents: []*entity.Document{{
            Info:          knowledgeModel.Info{Name: "notes.docx"},
            KnowledgeID:   7000,
            FileExtension: parser.FileExtensionDocx,
            URI:           "any",
        }},
    })
    require.NoError(t, err)
    require.NotNil(t, fc.lastCreateDocReq)
    assert.Nil(t, fc.lastCreateDocReq.OcrModelID,
        "non-PDF uploads must not attach ocr_model_id — text_source schema would reject it")
}

func TestCreateDocument_OCRDefault_EmptyEnv_NoAttach(t *testing.T) {
    fc := &fakeClient{
        nextKBID:   "kb-uuid",
        nextDocID:  "doc-uuid",
        nextTaskID: "task-uuid",
    }
    i := newTestImpl(t, fc, 8804)
    i.defaultOCRModelID = "" // simulate empty env
    seedKBMapping(t, i.mapping, 7000, "kb-uuid")

    _, err := i.CreateDocument(context.Background(), &service.CreateDocumentRequest{
        Documents: []*entity.Document{{
            Info:          knowledgeModel.Info{Name: "x.pdf"},
            KnowledgeID:   7000,
            FileExtension: parser.FileExtensionPDF,
            URI:           "any",
        }},
    })
    require.NoError(t, err)
    require.NotNil(t, fc.lastCreateDocReq)
    assert.Nil(t, fc.lastCreateDocReq.OcrModelID,
        "empty env default must not produce a top-level ocr_model_id (would 40001 on text_source PDFs)")
}
```

If `lastCreateDocReq` does not yet exist on `fakeClient`, also extend the stub: find `type fakeClient struct` (in `knowledge_test.go`) and add:

```go
lastCreateDocReq *contract.CreateDocumentRequest
```

In `fakeClient.CreateDocument` (same file, the stub method) capture: `f.lastCreateDocReq = req` at the top of the function body.

If a `seedKBMapping(t, mapping, cozeID, ragKBID)` helper does not yet exist, find a recent test (e.g. `TestCreateDocument_InsertsMapping`) and copy its mapping-insertion shape into a helper at the bottom of `knowledge_test.go`:

```go
func seedKBMapping(t *testing.T, m *MappingRepo, cozeID int64, ragKBID string) {
    t.Helper()
    nowMs := time.Now().UnixMilli()
    require.NoError(t, m.InsertKB(context.Background(), cozeID, ragKBID, "test-tenant", nowMs))
}
```

(Imports: `"context"`, `"time"`, `"github.com/stretchr/testify/require"` — add if missing.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./domain/knowledge/service/ragimpl/... -run "TestCreateDocument_OCRDefault" -v`
Expected: FAIL — `fc.lastCreateDocReq.OcrModelID` is always `nil` because `CreateDocument` does not yet set it.

- [ ] **Step 3: Wire injection in `CreateDocument`**

Open `backend/domain/knowledge/service/ragimpl/document.go`. Find `CreateDocument` (around line 259). Locate the block that constructs `ragReq := &contract.CreateDocumentRequest{ ... }` (around line 342). Immediately **before** that block, after the `applyDocumentOptionsOverrides` handling (around line 324), add:

```go
// PDF uploads: rag's auto-detector (services/document_service.py
// inspect_pdf_source_modality) silently promotes no-text-layer PDFs from
// text_source to scanned_document_source, after which the scanned-schema
// validator requires `ocr_model_id`. If the upload form already supplied
// one inside document_options, rag's flat_options fallback handles it and
// we leave the top level nil so the form value wins (per
// models/value_objects/ingestion_request.py to_resolver_payload). Else,
// when a config default is configured, inject it at the top level so the
// promoted-to-scanned path validates.
var ocrModelID *string
if d.FileExtension == parser.FileExtensionPDF &&
    i.defaultOCRModelID != "" &&
    !documentOptionsHasOCRModelID(documentOptions) {
    v := i.defaultOCRModelID
    ocrModelID = &v
}
```

Then add `OcrModelID: ocrModelID,` to the `ragReq` literal. The final block should look like:

```go
ragReq := &contract.CreateDocumentRequest{
    FileBytes:            fileBytes,
    Filename:             d.Name,
    FileType:             string(d.FileExtension),
    SourceModality:       sourceModality,
    ChunkSize:            chunkSize,
    ChunkOverlap:         chunkOverlap,
    ExtraMetadata:        extraMetadata,
    EnableOCR:            enableOCR,
    EnableImageEmbedding: enableImageEmbedding,
    DocumentOptions:      documentOptions,
    OcrModelID:           ocrModelID,
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./domain/knowledge/service/ragimpl/... -run "TestCreateDocument_OCRDefault" -v`
Expected: PASS (all four).

- [ ] **Step 5: Run the full ragimpl test sweep**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./domain/knowledge/service/ragimpl/... -v`
Expected: PASS for all tests.

- [ ] **Step 6: Commit**

```bash
git add backend/domain/knowledge/service/ragimpl/document.go backend/domain/knowledge/service/ragimpl/document_test.go backend/domain/knowledge/service/ragimpl/knowledge_test.go
git commit -m "feat(knowledge-rag): attach DefaultOCRModelID for PDF uploads"
```

---

## Task 7: Update env files

The OCR model id needs to be present in the env-var bundles for each deployment mode. Value matches the only active OCR provider in rag's `model_providers.json` (verified live: `model-ocr-paddle-infer-text`).

**Files:**
- Modify: `docker/.env.v2`
- Modify: `docker/.env.debug`
- Modify: `docker/.env.debug.example`

---

- [ ] **Step 1: Add to `.env.v2`**

Open `docker/.env.v2`. Find the existing `RAG_DEFAULT_*_MODEL_ID` block (the two embedding ones plus the LLM/rerank ones added previously). Append:

```bash
export RAG_DEFAULT_OCR_MODEL_ID="model-ocr-paddle-infer-text"
```

- [ ] **Step 2: Add to `.env.debug`**

Open `docker/.env.debug`. Find the same block. Append the same line.

- [ ] **Step 3: Add to `.env.debug.example`**

Open `docker/.env.debug.example`. Find the same block. Append the same line.

- [ ] **Step 4: Commit**

```bash
git add docker/.env.v2 docker/.env.debug docker/.env.debug.example
git commit -m "chore(docker): set RAG_DEFAULT_OCR_MODEL_ID for v2 + local stacks"
```

---

## Task 8: Wider sweep + manual smoke

Confirm nothing else broke. The fix only takes effect on PDF uploads where the form didn't supply ocr_model_id; existing scanned uploads (form supplied) and existing text/docx uploads (no PDF) must continue to work as before.

**Files:** none (verification only).

---

- [ ] **Step 1: Wider `go vet` sweep**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go vet ./conf/rag/... ./infra/contract/rag/... ./infra/rag/... ./domain/knowledge/service/ragimpl/... ./application/knowledge/...`
Expected: no output.

- [ ] **Step 2: Wider build sweep**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./...`
Expected: no errors.

- [ ] **Step 3: Wider test sweep**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./conf/rag/... ./infra/contract/rag/... ./infra/rag/... ./domain/knowledge/service/ragimpl/... ./application/knowledge/...`
Expected: PASS.

- [ ] **Step 4: Restart `coze-server-v2`**

Run:
```bash
docker compose -f docker/docker-compose.v2.yml up -d coze-server-v2
```

Verify env propagation:
```bash
docker exec coze-server-v2 sh -c 'echo "[$RAG_DEFAULT_OCR_MODEL_ID]"'
```
Expected: `[model-ocr-paddle-infer-text]`.

- [ ] **Step 5: Manual smoke**

In the web UI, upload `windowSticker5N1AT2MV3FC845201_windowSticker.pdf` (the original failing case from the spec) to a text KB as **文本 PDF**.

Expected:
- No `rag 40001` error in the UI
- The document appears in the KB list and progresses through indexing
- After ingestion, retrieving via the workflow knowledge-retrieve node returns hits (OCR'd text chunks)

If the upload still 40001s, check `docker logs --since 5m rag-web` for the actual rag-side error code/message — it should NOT be "ocr_model_id is required" anymore. Any new error is a different bug; surface it and stop.

- [ ] **Step 6: Open PR**

```bash
git push -u origin "$(git branch --show-current)"
gh pr create --title "feat(knowledge-rag): attach default ocr_model_id for PDF uploads" --body "$(cat <<'EOF'
## Summary

- Coze backend attaches the env-driven `RAG_DEFAULT_OCR_MODEL_ID` to the multipart top-level `ocr_model_id` field for PDF uploads where the dynamic upload form did not supply one.
- After rag's silent auto-promote from `text_source` to `scanned_document_source` (on no-text-layer PDFs), the scanned-schema validator's `ocr_model_id is required when enable_ocr is true` check now passes instead of 40001-ing.
- Existing scanned uploads (form supplied `ocr_model_id` in `document_options`) are untouched — rag's `flat_options.get("ocr_model_id")` fallback continues to honour the form value.

Spec: `docs/superpowers/specs/2026-05-21-coze-pdf-ocr-default-modelid-design.md`

## Test plan

- [x] `go test ./conf/rag/... ./infra/contract/rag/... ./infra/rag/... ./domain/knowledge/service/ragimpl/... ./application/knowledge/...` passes
- [x] Manual: upload windowSticker as 文本 PDF — succeeds, document indexed
- [x] Manual: upload windowSticker as 扫描件 — still works (no regression)
- [x] Manual: upload docx/txt — still works (no top-level ocr_model_id sent)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Self-Review Map (spec section → plan task)

- §Design / Config (`backend/conf/rag/config.go` + `rag.yaml`) → Task 1
- §Design / Wire (`types.go` + `client.go`) → Task 4
- §Design / Domain wiring (`factory.go` + `init.go` + `document.go`) → Tasks 2, 3, 5, 6
- §Design / Env files (`.env.v2` + `.env.debug` + `.env.debug.example`) → Task 7
- §Design / Tests (4 scenarios) → Task 6 Step 1
- §Risks / startup logging — out of scope here (a follow-up if RAG_DEFAULT_OCR_MODEL_ID drift becomes a real incident; surfacing in init.go warnings is a 3-line addition then)
- §Acceptance (re-upload windowSticker, no 40001, OCR'd text chunks) → Task 8 Step 5
