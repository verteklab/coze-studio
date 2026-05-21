# Coze attaches default ocr_model_id for PDF uploads

**Date:** 2026-05-21
**Author:** xinyu.liu@vorteklab.com
**Status:** Design — pending user review

## Problem

Uploading `windowSticker5N1AT2MV3FC845201_windowSticker.pdf` (a window-sticker PDF that is image-only — no extractable text layer) through the coze upload flow as **"文本 PDF"** fails with:

```
invalid parameter : rag 40001: 请求参数无效
```

rag-web's log reveals the real error:

```
Domain error on POST .../documents code=40001: ocr_model_id is required
```

Trace:

1. Coze (`ragimpl.CreateDocument` → `strategyToRagFields`) sends `source_modality=text_source`, no `enable_ocr`, no `ocr_model_id`.
2. rag-web (`services/document_service.py:121`) runs `inspect_pdf_source_modality(file_bytes)` and detects no text layer → returns `SOURCE_MODALITY_SCANNED`.
3. `_apply_detected_upload_source(...)` in the same service silently **promotes** the request from `text_source` to `scanned_document_source`.
4. The scanned-document schema defaults `enable_ocr=True`; `_apply_schema_defaults` therefore sets `enable_ocr=True` on the payload.
5. `_validate_ocr_image_flags` (`policy/validators/ingestion_validator.py:184`) sees `enable_ocr=True` and an empty `ocr_model_id` → 40001.

Uploading the same file as **"扫描件"** succeeds because the dynamic upload form supplies `ocr_model_id` inside `document_options`, and rag's `to_resolver_payload` falls back to `flat_options.get("ocr_model_id")` when the top-level field is empty (see `models/value_objects/ingestion_request.py:98`).

Memory entry [rag-pdf-modality-auto-detect](../../../../.claude/projects/-home-xinyuliu-coze-studio/memory/rag-pdf-modality-auto-detect.md) documents the same auto-promote-silently behavior; on 2026-05-20 the user accepted the status quo because no concrete failure case had surfaced. windowSticker is that failure case.

## Goal

For every PDF upload that does **not** already carry an `ocr_model_id` (in `document_options` or via the dynamic form), coze backend transparently attaches a configured default OCR model id to the multipart top-level `ocr_model_id` field. Result: rag's auto-promote from `text_source` to `scanned_document_source` has the ingredient it needs; uploads no longer 40001; the user's frontend choice (文本 PDF vs 扫描件) does not need to change.

## Non-Goals

- **No coze-side PDF text-layer probe.** rag already runs PyMuPDF/pdfium on the bytes; coze adding a parallel Go-side detector would drift over time. Coze stays modality-agnostic and rag arbitrates.
- **No retrieval-side change.** `RAG_DEFAULT_LLM_MODEL_ID` and `RAG_DEFAULT_RERANK_MODEL_ID` env vars stay. Rag's retrieval resolver has no `flat_options`-style fallback for `query_strategy.llm_model_id` / `rerank_model_id`, so coze must continue to supply them per-request, and the env-driven defaults remain the supply.
- **No rag source change.** The user does not own the rag codebase for this engagement; the fix lives entirely in coze.
- **No frontend change.** The 文本 PDF / 扫描件 radio, the OCR toggle, the dynamic parsing panel — all untouched. Behavior change is transparent to the user.
- **No KB-side OCR binding.** rag's `CreateKnowledgeBaseRequest` schema accepts only `text_embedding_model_id` and `image_embedding_model_id`. Adding `ocr_model_id` at KB level would require rag changes; out of scope.

## Design

### Config

`backend/conf/rag/config.go` `Config` gains:

```go
// DefaultOCRModelID is the OCR model id coze attaches as the multipart
// top-level `ocr_model_id` for PDF uploads when the dynamic form did not
// supply one. Required because rag's PDF auto-detector (services/
// document_service.py:121 inspect_pdf_source_modality) silently promotes
// no-text-layer PDFs from `text_source` to `scanned_document_source`,
// after which the scanned-schema validator requires `ocr_model_id`.
// Empty value disables the attachment — upload still works for ordinary
// text PDFs, but image-only PDFs will continue to 40001.
DefaultOCRModelID string `yaml:"default_ocr_model_id"`
```

`backend/conf/rag/rag.yaml`:

```yaml
rag:
  # ... existing fields ...
  # OCR model id attached to multipart top-level `ocr_model_id` for PDF
  # uploads when the upload form did not supply one. See config.go comment.
  default_ocr_model_id: "${RAG_DEFAULT_OCR_MODEL_ID}"
```

### Wire

`backend/infra/contract/rag/types.go` `CreateDocumentRequest` gains an optional pointer field (mirroring the existing `EnableOCR *bool` style — nil means "do not write the multipart field"):

```go
// Optional: rag's `ocr_model_id` multipart form field. nil means "do not
// write the field" — rag will fall back to flat_options.get("ocr_model_id")
// from the document_options blob (see rag's
// models/value_objects/ingestion_request.py:98). Coze sets this for PDF
// uploads where document_options does not already carry the key, so rag's
// validator passes after auto-promote to scanned_document_source.
OcrModelID *string
```

`backend/infra/rag/client.go` writes the field next to the existing `enable_ocr` block:

```go
if req.OcrModelID != nil {
    if err := w.WriteField("ocr_model_id", *req.OcrModelID); err != nil {
        return nil, fmt.Errorf("multipart write ocr_model_id: %w", err)
    }
}
```

### Domain wiring

`backend/domain/knowledge/service/ragimpl/factory.go` `Impl` gains `defaultOCRModelID string`, threaded in from `application/knowledge/init.go` the same way `defaultLLMModelID` and `defaultRerankModelID` already are.

`backend/domain/knowledge/service/ragimpl/document.go` `CreateDocument` — after the `applyDocumentOptionsOverrides` block that produces `documentOptions` (the cleaned JSON string) and before `ragReq := &contract.CreateDocumentRequest{ ... }`:

```go
// PDF uploads: rag's auto-promote (inspect_pdf_source_modality) silently
// switches text_source → scanned_document_source when the PDF has no
// extractable text layer, after which validator requires `ocr_model_id`.
// If the upload form already put an ocr_model_id in document_options,
// rag's to_resolver_payload falls back to flat_options for it — leave
// alone. Otherwise inject our env-configured default at the top-level
// multipart field so the promoted-to-scanned path validates.
var ocrModelID *string
if d.FileExtension == parser.FileExtensionPDF &&
    i.defaultOCRModelID != "" &&
    !documentOptionsHasOCRModelID(documentOptions) {
    v := i.defaultOCRModelID
    ocrModelID = &v
}
```

Then add `OcrModelID: ocrModelID,` to `ragReq`.

`documentOptionsHasOCRModelID` is a small helper next to `applyDocumentOptionsOverrides` in the same file:

```go
// documentOptionsHasOCRModelID reports whether a non-empty `ocr_model_id`
// key is present in the JSON object. Empty options or parse failure → false
// (we err on attaching the default; rag's validator drops it for text PDFs
// via _apply_detected_upload_source.clear_ocr_model_id anyway).
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

### Env files

`docker/.env.v2`, `docker/.env.debug`, `docker/.env.debug.example` get:

```bash
export RAG_DEFAULT_OCR_MODEL_ID="model-ocr-paddle-infer-text"
```

(Value matches the only active OCR provider in the deployed rag's `model_providers.json`. The existing memory entry [coze-photo-extract-caption-not-migrated](../../../../.claude/projects/-home-xinyuliu-coze-studio/memory/coze-photo-extract-caption-not-migrated.md) confirms this is the OCR id in service.)

### Tests

`backend/domain/knowledge/service/ragimpl/document_test.go` (extend the existing CreateDocument test or add a sibling):

| Scenario | Expected wire shape |
|---|---|
| PDF, no `document_options.ocr_model_id`, env default set | `ragReq.OcrModelID == &"model-ocr-paddle-infer-text"` |
| PDF, `document_options` has `ocr_model_id`, env default set | `ragReq.OcrModelID == nil` (let rag's flat_options fallback take it) |
| PDF, env default empty | `ragReq.OcrModelID == nil` (graceful degrade — same 40001 risk as today for no-text PDFs, but no worse) |
| Non-PDF (docx, txt, md) | `ragReq.OcrModelID == nil` (text_source schema would 40001 on `ocr_model_id requires enable_ocr=true`) |
| Image (jpg/png) | `ragReq.OcrModelID == nil` (existing image_source flow already provides ocr_model_id via document_options form path) |

Acceptance: re-upload `windowSticker5N1AT2MV3FC845201_windowSticker.pdf` as 文本 PDF — succeeds, document indexed with OCR text chunks.

## Risks & open questions

- **Env default mismatched with deployed rag model id.** If the operator forgets to update `RAG_DEFAULT_OCR_MODEL_ID` after a rag model rebrand, every promote-to-scanned upload 40001s on `model not found`. Mitigation: log a startup line listing the value when non-empty; surface in the existing `init.go` warnings.
- **Frontend may eventually want to display the default value too.** Out of scope; the existing dynamic-form `ocr_model_id` input still wins via document_options.

## Predecessor / related memory

- [rag-pdf-modality-auto-detect](../../../../.claude/projects/-home-xinyuliu-coze-studio/memory/rag-pdf-modality-auto-detect.md) — original status-quo decision (2026-05-20); this spec revisits it.
- [coze-rag-ocr-validator-mutex](../../../../.claude/projects/-home-xinyuliu-coze-studio/memory/coze-rag-ocr-validator-mutex.md) — prior image-KB OCR-mutex fix; documents the validator's enable_ocr ↔ ocr_model_id constraint this spec exploits.
- 2026-05-20-image-upload-force-ocr-on-design.md — earlier spec for image schemas; uses the same env-driven default pattern (different field).
