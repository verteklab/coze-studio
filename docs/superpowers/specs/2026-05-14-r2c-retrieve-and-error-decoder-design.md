# R2-C: Retrieve.query_image object + union-friendly error decoder

**Date:** 2026-05-14
**Status:** Draft
**Predecessor:** `2026-05-14-r2b-readpath-realignment-design.md` (R2-B)
**Sibling slices:** R2-D (new endpoints), R2-E (broader test scaffolding) — deferred to follow-on specs

## 1. Motivation

The 2026-05-14 round-2 contract audit (against rag `0e1f49b`) left two unfixed gaps after R2-A and R2-B landed:

1. **`Retrieve.query_image` shape mismatch.** Rag changed the field from a bare base64 string (`*string`) to an object (`ImageQueryDTO`) with `{image_base64, image_ref}` since the audit baseline. `RetrievalRequest` is now a `StrictBaseModel` with `extra="forbid"`, so any request that sends `"query_image": "<base64>"` is rejected with HTTP 422 before reaching the handler. Coze's contract still encodes the bare-string shape; any image-search retrieval returns 422. Pure-text retrieval still works because coze's `Retrieve` request omits `query_image` when nil.

2. **Error envelope decoder mismatch.** Rag emits TWO real error envelope shapes today: (a) flat `{code, message, data, request_id}` for business errors via `app/middlewares/exception_middleware.py`, and (b) pydantic 422 `{detail: [{loc, msg, type, ctx}]}` for validation errors. Coze's `ErrorBody` is hard-coded to FastAPI's default HTTPException `{detail: {code, message}}` — a shape rag does not emit. The result: `errBody.Detail.Code` and `errBody.Detail.Message` zero-decode on every rag error, and `MapRagError(status, 0, "")` always falls into `ErrRagUpstreamUnavailableCode`. The user has no visibility into the actual rag-side cause and no automated test catches the misclassification. The 2026-05-13 smoke wall ("rag service unavailable: http=500 rag=0 msg=") was a direct symptom — debugging required reading rag's logs manually.

R2-C addresses both. The error decoder fix is the leverage piece (every rag endpoint's error path benefits); the `query_image` fix is narrow but unblocks image search.

## 2. Goals & non-goals

### Goals

- `contract.RetrieveRequest.QueryImage` is `*QueryImage` matching rag's `ImageQueryDTO`; an image-search request reaches rag without `extra="forbid"` rejection.
- Errors returned from rag — including pydantic 422 validation failures and flat-envelope business errors — surface the actual rag code and message to coze's `MapRagError`, which classifies them correctly (pydantic 422 → `ErrKnowledgeInvalidParamCode`; rag's 40001-40009 / 40400-range / 40900-range → existing classifications; non-JSON / unknown → fall through to `ErrRagUpstreamUnavailableCode` as today).
- Both `doJSON` (via `doOnce`) and `doMultipart` paths use the same error decoder via a single helper function.
- Wire-shape tests in `client_test.go` lock the new behaviour: at least one happy-path Retrieve test asserting the object shape, plus tests covering pydantic 422 → InvalidParam classification end-to-end.

### Non-goals

- `MapRagError` itself is unchanged. Pydantic 422 maps to InvalidParam by having the decoder synthesize a virtual rag code `40001`; this is a deliberate small hack to keep `MapRagError` stable. (Q2 in brainstorm: "read as virtual code 40001, block into existing 40001-40009 → InvalidParam branch.")
- No new errno values (e.g. no `ErrRagValidationFailed`) — defers to a future scope decision when upstream/local validation needs to be distinguished by the application layer.
- No frontend code changes. The image-search query path is not currently exposed through coze's UI; this spec only unblocks it server-side. When the UI lands an image-search entry, it will pass a `query_image.image_base64` string up through the existing service layer.
- R2-D's `/capabilities` endpoint (which would let coze pre-validate retrievers) is out of scope.
- R2-E's broader httptest scaffolding is out of scope; this spec adds focused tests only for what changes.

## 3. Contract change

### 3.1 Rag's authoritative shapes (frozen as of `0e1f49b`)

**`POST /api/v1/retrieval`** — request body's `query_image` field (per `app/api/schemas/retrieval.py:14-24`):

```python
class ImageQueryDTO(StrictBaseModel):
    image_base64: Optional[str] = None
    image_ref: Optional[str] = None
```

`StrictBaseModel` enforces `extra="forbid"`; both fields are optional but `_has_query_input()` requires at least one to be non-empty for the request to be accepted.

**Error envelopes** — three real shapes coze must decode:

(a) **Flat business envelope** (rag's `app/middlewares/exception_middleware.py`):

```json
{"code": 50001, "message": "model not found", "data": null, "request_id": "..."}
```

(b) **FastAPI HTTPException** — emitted when handlers raise `HTTPException(status_code=N, detail={"code": X, "message": Y})`:

```json
{"detail": {"code": 40001, "message": "X-Tenant-Id header is required"}}
```

(c) **Pydantic 422 validation failure**:

```json
{"detail": [{"loc": ["body", "query_image", "image_base64"], "msg": "field required", "type": "value_error.missing", "ctx": {}}]}
```

### 3.2 Coze-side after R2-C

`contract.RetrieveRequest.QueryImage` becomes `*QueryImage` (was `*string`). New `QueryImage` struct lives in `types.go`:

```go
// QueryImage mirrors rag's ImageQueryDTO. At least one field must be non-empty
// for rag's RetrievalRequest._has_query_input() to accept; coze does not
// pre-validate the constraint — rag's pydantic 422 surfaces back via
// DecodeErrorEnvelope and MapRagError reports it as InvalidParam.
type QueryImage struct {
    ImageBase64 string `json:"image_base64,omitempty"`
    ImageRef    string `json:"image_ref,omitempty"`
}
```

The old `ErrorBody` and `ErrorDetail` types in `types.go` are deleted: their only callers (`client.go:160`, `client.go:238`) switch to the new helper.

### 3.3 New error-decoder helper

A new file `backend/infra/contract/rag/errors.go` exposes:

```go
// DecodeErrorEnvelope parses a rag error response body into a (code, message)
// pair suitable for MapRagError. It tolerates rag's three real envelope
// shapes (see spec §3.1) and returns (0, "") on a body that matches none of
// them — letting MapRagError fall through to ErrRagUpstreamUnavailableCode,
// the same behaviour as today's broken decoder so we never regress.
//
// Pydantic 422 detail arrays are translated to a synthetic code 40001 with a
// formatted message "<dotted-loc>: <msg>"; MapRagError's existing
// 40001-40009 → InvalidParam branch picks them up naturally without
// requiring an explicit "is-pydantic" sentinel.
func DecodeErrorEnvelope(raw []byte) (code int, message string)
```

## 4. Architecture

### 4.1 Flow (error path)

```
http response status >= 400
  → client.go::doOnce or doMultipart reads body bytes
    → contract.DecodeErrorEnvelope(raw) → (code, message)
        → tries flat envelope, then FastAPI HTTPException, then pydantic 422 array
        → pydantic 422: code = 40001, message = "body.query_image.image_base64: field required"
        → unknown / non-JSON: returns (0, "")
    → MapRagError(httpStatus, code, message)
        → 40001-40009 → ErrKnowledgeInvalidParamCode
        → 40400-range → ErrKnowledge[Document]NotExistCode
        → 40900-range → ErrKnowledgeDuplicateCode
        → default → ErrRagUpstreamUnavailableCode
  ← returns errno-tagged error to caller
```

### 4.2 Flow (image-search retrieve)

```
domain code calls ragimpl.Retrieve with image bytes
  → builds contract.RetrieveRequest{
      KBIDs:      [...],
      QueryImage: &contract.QueryImage{ImageBase64: base64.StdEncoding.EncodeToString(imgBytes)},
      ...
    }
  → client.Retrieve → doJSON → rag's pydantic accepts the object shape
  ← RetrieveResponse decoded normally
```

If the caller forgets `ImageBase64`/`ImageRef` (both empty), rag rejects with pydantic 422 ("at least one of image_base64/image_ref required"); R2-C's new decoder surfaces that as InvalidParam.

### 4.3 Touched files

| File | Change |
|---|---|
| `backend/infra/contract/rag/types.go` | Delete `ErrorBody` + `ErrorDetail`. Add `QueryImage` struct. Change `RetrieveRequest.QueryImage` to `*QueryImage`. |
| `backend/infra/contract/rag/errors.go` (NEW) | `DecodeErrorEnvelope` + helper `formatPydanticDetail`. |
| `backend/infra/contract/rag/errors_test.go` (NEW) | Table-driven unit tests for the decoder. |
| `backend/infra/rag/client.go` | `doOnce` (line 160) and `doMultipart` (line 238) replace `var errBody contract.ErrorBody; _ = json.Unmarshal(raw, &errBody); return MapRagError(resp.StatusCode, errBody.Detail.Code, errBody.Detail.Message)` with `code, msg := contract.DecodeErrorEnvelope(raw); return MapRagError(resp.StatusCode, code, msg)`. |
| `backend/infra/rag/client_test.go` | New tests: `TestRetrieve_QueryImageObject` (image-search wire-shape lock), `TestClient_DecodesFlatEnvelopeError` (5xx + flat body → upstream-unavailable with correct rag code), `TestClient_DecodesPydantic422AsInvalidParam` (422 + detail array → ErrKnowledgeInvalidParamCode). |
| `backend/infra/rag/errors.go` | **Unchanged.** `MapRagError` doesn't grow new branches. |

## 5. Components

### 5.1 `DecodeErrorEnvelope` implementation outline

```go
// DecodeErrorEnvelope parses ... (see godoc above)
func DecodeErrorEnvelope(raw []byte) (code int, message string) {
    if len(bytes.TrimSpace(raw)) == 0 {
        return 0, ""
    }

    // (a) Flat envelope {code, message, data, request_id}
    var flat struct {
        Code    int    `json:"code"`
        Message string `json:"message"`
    }
    if err := json.Unmarshal(raw, &flat); err == nil && (flat.Code != 0 || flat.Message != "") {
        return flat.Code, flat.Message
    }

    // (b) FastAPI HTTPException {detail: {code, message}}
    var fastapi struct {
        Detail struct {
            Code    int    `json:"code"`
            Message string `json:"message"`
        } `json:"detail"`
    }
    if err := json.Unmarshal(raw, &fastapi); err == nil && (fastapi.Detail.Code != 0 || fastapi.Detail.Message != "") {
        return fastapi.Detail.Code, fastapi.Detail.Message
    }

    // (c) Pydantic 422 {detail: [{loc, msg, ...}]}
    var pydantic struct {
        Detail []struct {
            Loc []any  `json:"loc"`
            Msg string `json:"msg"`
        } `json:"detail"`
    }
    if err := json.Unmarshal(raw, &pydantic); err == nil && len(pydantic.Detail) > 0 {
        d := pydantic.Detail[0]
        return 40001, formatPydanticDetail(d.Loc, d.Msg)
    }

    return 0, ""
}

// formatPydanticDetail renders loc as a dotted path joined with the msg.
//   loc=["body", "query_image", "image_base64"], msg="field required"
//     → "body.query_image.image_base64: field required"
// loc entries can be strings or ints (index into a list); both are stringified.
func formatPydanticDetail(loc []any, msg string) string { ... }
```

**Ordering rationale:** flat envelope first because it's the common case for rag's runtime errors. FastAPI HTTPException second because rag does still raise these for things like missing tenant headers. Pydantic 422 last and unconditionally returns code 40001 because it represents request validation failure.

**Safety:** the unmarshal-then-check-for-non-zero pattern protects against ambiguity. A pydantic 422 body (`{"detail": [...]}`) fails to unmarshal against the FastAPI shape because `Detail` is typed as a struct there but the wire value is an array — Go's `encoding/json` returns a type-mismatch error. The `err == nil && ...` guard short-circuits on that error and falls through to case (c), where the array shape matches. Symmetrically, a flat envelope without `code`/`message` (e.g. just `{"data": null}`) unmarshals cleanly into the flat struct but yields zero values; the `(code != 0 || message != "")` guard prevents a false positive and lets case (b) try next.

### 5.2 `formatPydanticDetail` behaviour

- `loc = ["body", "x"]`, `msg = "missing"` → `"body.x: missing"`
- `loc = ["body", "items", 0, "name"]`, `msg = "required"` → `"body.items.0.name: required"` (integers stringified)
- `loc = []`, `msg = "x"` → `": x"` (degenerate; acceptable)
- `loc = nil`, `msg = ""` → `": "` (caller already gated by `len(pydantic.Detail) > 0`, so this only fires if the array entry itself is empty)

### 5.3 `QueryImage` struct

Defined in `types.go` next to `RetrieveRequest`. Both fields use `omitempty` so a `&QueryImage{ImageRef: "..."}` doesn't include `image_base64: ""` on the wire (rag's `extra="forbid"` allows the field but `_has_query_input` would consider empty-string non-input).

### 5.4 Caller updates

**`RetrieveRequest.QueryImage` field**:

```go
// before:
QueryImage *string `json:"query_image,omitempty"`

// after:
QueryImage *QueryImage `json:"query_image,omitempty"`
```

No production caller exists (grep confirmed); only the field definition itself referenced the old type. Any future caller wraps:

```go
req := &contract.RetrieveRequest{
    KBIDs:      []string{kbID},
    QueryImage: &contract.QueryImage{ImageBase64: b64},
}
```

**Error decode in `client.go`** — two identical replacement points, both at lines that currently look like:

```go
var errBody contract.ErrorBody
_ = json.Unmarshal(raw, &errBody)
return MapRagError(resp.StatusCode, errBody.Detail.Code, errBody.Detail.Message)
```

Become:

```go
code, msg := contract.DecodeErrorEnvelope(raw)
return MapRagError(resp.StatusCode, code, msg)
```

## 6. Data flow & invariants

- **Unknown body → graceful fallback.** Any body that matches none of the three shapes (including non-JSON, truncated JSON, empty body) yields `(0, "")`. `MapRagError(httpStatus, 0, "")` returns `ErrRagUpstreamUnavailableCode` — identical to today's behaviour for unrecognised responses. No regression.
- **Synthetic code 40001 is not a wire code.** It's an internal convention between `DecodeErrorEnvelope` and `MapRagError` for "rag's validation layer rejected the request." Future maintainers reading `MapRagError`'s switch should not assume rag itself emits this code on pydantic failures — rag's pydantic emits HTTP 422 with the detail array, not an explicit `code` field.
- **`QueryImage` validation lives on the rag side.** Coze does not pre-validate "at least one of image_base64/image_ref non-empty." Rag's `_has_query_input` does (and pydantic 422 surfaces the rejection). This avoids duplicating validation logic and lets coze stay thin.

## 7. Error handling

| Scenario | Behaviour |
|---|---|
| rag returns 5xx + flat envelope `{code: 50001, message: "model not found"}` | DecodeErrorEnvelope returns (50001, "model not found"); MapRagError returns `ErrRagUpstreamUnavailableCode` with msg preserved (5xxx is default branch). |
| rag returns 40001-40009 flat or FastAPI shape | DecodeErrorEnvelope returns (4000X, msg); MapRagError returns `ErrKnowledgeInvalidParamCode`. |
| rag returns 422 + pydantic detail array | DecodeErrorEnvelope returns (40001, "loc.path: msg"); MapRagError returns `ErrKnowledgeInvalidParamCode`. ← **fixed by R2-C; before, would have returned UpstreamUnavailable.** |
| rag returns 404 with `{detail: {code: 40401, message: "kb not found"}}` | DecodeErrorEnvelope returns (40401, "kb not found"); MapRagError returns `ErrKnowledgeNotExistCode`. |
| rag returns non-JSON HTML (e.g. reverse proxy 502) | DecodeErrorEnvelope returns (0, ""); MapRagError returns `ErrRagUpstreamUnavailableCode` with `msg=http=502 rag=0 msg=`. Same as today; logs the http status for debugging. |
| Empty body | Same as non-JSON. |

## 8. Testing

### 8.1 Unit tests for `DecodeErrorEnvelope`

New file `backend/infra/contract/rag/errors_test.go`, table-driven:

```go
tests := []struct {
    name        string
    raw         string
    wantCode    int
    wantMessage string
}{
    {"flat envelope", `{"code":50001,"message":"model not found"}`, 50001, "model not found"},
    {"FastAPI HTTPException", `{"detail":{"code":40001,"message":"X-Tenant-Id required"}}`, 40001, "X-Tenant-Id required"},
    {"pydantic 422 single", `{"detail":[{"loc":["body","x"],"msg":"missing"}]}`, 40001, "body.x: missing"},
    {"pydantic 422 nested loc", `{"detail":[{"loc":["body","items",0,"name"],"msg":"required"}]}`, 40001, "body.items.0.name: required"},
    {"pydantic 422 takes first entry", `{"detail":[{"loc":["a"],"msg":"first"},{"loc":["b"],"msg":"second"}]}`, 40001, "a: first"},
    {"empty body", ``, 0, ""},
    {"non-JSON HTML", `<html>...`, 0, ""},
    {"flat with only message", `{"message":"plain text error"}`, 0, "plain text error"},
    {"unknown shape", `{"foo":"bar"}`, 0, ""},
}
```

### 8.2 httptest contract tests

Three new tests in `backend/infra/rag/client_test.go`:

**`TestRetrieve_QueryImageObject`** — handler asserts request body has `query_image.image_base64 == "abc"` (parses JSON, navigates the nested object). Returns a valid `RetrieveResponse` envelope; assert decode succeeds. Locks the wire shape.

**`TestClient_DecodesFlatEnvelopeError`** — server returns 500 + flat `{code: 50001, message: "model not found"}`. Call `Client.ListModelProviders` (a GET endpoint, simplest to exercise `doOnce`'s error branch); assert the returned error contains the rag code and message and classifies as `ErrRagUpstreamUnavailableCode` (5xxxx is default branch).

**`TestClient_DecodesPydantic422AsInvalidParam`** — server returns 422 + pydantic detail array. Call `Client.CreateKB` (POST; exercises `doOnce`'s error branch with a request body). Assert the returned error classifies as `ErrKnowledgeInvalidParamCode` (the fix). Also assert the error message contains the formatted "loc.path: msg" string for debuggability.

### 8.3 Existing tests

`TestEnvelope_NonZeroCodeIsError` and other existing tests in `client_test.go` continue to pass (they exercise the 2xx-with-non-zero-envelope-code path which goes through `MapRagError(status, env.Code, env.Message)` — different branch from the 4xx/5xx path R2-C touches). Verify in plan.

### 8.4 Smoke (optional)

Trigger an image-search retrieval from rag's `/api/v1/retrieval` endpoint with an empty `query_image` to confirm the pydantic 422 path. Compare coze's error log before vs after — message should change from `rag service unavailable: http=422 rag=0 msg=` to `rag service unavailable: http=422 rag=40001 msg=body.query_image: at least one of image_base64 or image_ref must be set` or similar (depending on rag's exact pydantic message text).

This is exploratory only; not required for R2-C to merge.

## 9. Compatibility & rollout

- No schema change. No frontend change. No external API contract change.
- `ErrorBody` and `ErrorDetail` types are deleted; the only callers are the two `client.go` sites that get rewritten in the same commit. Cross-package compile breakage is not possible because they were package-private to `contract/rag`.
- `MapRagError` signature unchanged. Existing classifications unchanged.
- Errors that previously surfaced as `ErrRagUpstreamUnavailableCode` with `msg=http=4xx rag=0 msg=` will, post-R2-C, surface with their actual rag code and message preserved. This is a UX improvement, not a behaviour change for callers — they still get an error; only the diagnostics improve.

## 10. Open questions

None blocking the implementation plan.

Two minor items deferred to plan-time:

1. **Where exactly to put `DecodeErrorEnvelope`** — `contract/rag/errors.go` (recommended; new file) vs. inline in `contract/rag/types.go`. Either works; new file is cleaner because the file would otherwise grow each time a new contract type lands.
2. **Test ordering: error decoder commit vs. QueryImage commit** — the plan will sequence error decoder first so any QueryImage debugging surfaces clean rag errors. Both are independently testable.
