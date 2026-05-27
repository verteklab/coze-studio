# R2-C: Retrieve.query_image + union-friendly error decoder — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Spec:** `docs/superpowers/specs/2026-05-14-r2c-retrieve-and-error-decoder-design.md`
**Branch:** `feat/replace-knowledge-base` (continuation, base `9edd5eb3`)
**Goal:** Coze's rag client correctly decodes rag's actual error envelopes (flat, FastAPI HTTPException, pydantic 422 array) and routes pydantic 422 to `ErrKnowledgeInvalidParamCode`; and `Retrieve.query_image` matches rag's `ImageQueryDTO` object shape so image-search retrievals stop returning 422 at rag's validation layer.

**Architecture:** Two thematic commits.
1. **Error decoder union**: new `contract.DecodeErrorEnvelope(raw) → (code, message)` tries three envelope shapes in order (flat → FastAPI HTTPException → pydantic 422 array, with synthetic code `40001` for pydantic). Drops old `ErrorBody`/`ErrorDetail` types. `client.go::doOnce` and `doMultipart` both swap their inline decode for the helper. Unit tests cover all decoder branches; httptest tests confirm end-to-end that pydantic 422 classifies as `ErrKnowledgeInvalidParamCode`.
2. **`Retrieve.query_image` object shape**: add `QueryImage{ImageBase64, ImageRef}` struct, change `RetrieveRequest.QueryImage` from `*string` to `*QueryImage`, add httptest locking the wire shape.

**Tech Stack:** Go 1.24 (pinned via `GOTOOLCHAIN`), `encoding/json`, `net/http/httptest`, `errorx.StatusError` for code-level assertions in tests.

---

## Pre-flight: facts the plan depends on

Locked during plan-writing; capture so executing tasks don't need to re-discover.

- `backend/infra/contract/rag/types.go` current state (after R2-B):
  - `RetrieveRequest.QueryImage` at line 196: `QueryImage *string \`json:"query_image,omitempty"\``
  - `ErrorBody` at lines 227-234 (struct + comment block); `ErrorDetail` at lines 236-239. Both to be DELETED in Phase A.
- `backend/infra/rag/client.go`:
  - `doOnce` error decode at lines 159-166 (JSON path).
  - `doMultipart` error decode at lines 237-241 (multipart path).
  - Both decode `var errBody contract.ErrorBody; _ = json.Unmarshal(raw, &errBody); return MapRagError(resp.StatusCode, errBody.Detail.Code, errBody.Detail.Message)`. Both to be REPLACED in Phase A.
- `backend/infra/rag/errors.go::MapRagError` — UNCHANGED by this plan. Existing classification:
  - 40001-40009 → `ErrKnowledgeInvalidParamCode` (where pydantic 422 will land via synthetic code 40001)
  - 40401 → `ErrKnowledgeNotExistCode`; 40402 → `ErrKnowledgeDocumentNotExistCode`; other 404xx → `ErrKnowledgeNotExistCode`
  - 40900-40999 → `ErrKnowledgeDuplicateCode`
  - default → `ErrRagUpstreamUnavailableCode`
- errno values (`backend/types/errno/`):
  - `ErrKnowledgeInvalidParamCode = 105000000`
  - `ErrRagUpstreamUnavailableCode = 105100002`
- `errorx.StatusError` interface (`backend/pkg/errorx/error.go:30-36`) exposes `Code() int32`. Tests assert on errno via `errors.As(err, &se)` where `se errorx.StatusError`.
- `ErrorBody` callers (grep-confirmed):
  - `client.go:160`, `client.go:238` — the two sites being rewired.
  - Nothing else. (The `canvas.go:507` reference is a comment about a workflow-domain unrelated type.)
- `RetrieveRequest.QueryImage` callers (grep-confirmed): NONE in production code or tests. The field exists but no caller constructs a value for it.
- Rag's `ImageQueryDTO` (verified in `/Users/liuxinyu/workspace/rag/app/api/schemas/retrieval.py:14-24`): `image_base64: Optional[str] = None`, `image_ref: Optional[str] = None`, `StrictBaseModel` (extra="forbid").
- httptest test pattern in `client_test.go`: `c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})`. There's a `newTestClient` helper at the top of the file used by older tests; newer R2-A/R2-B tests use `New(...)` directly. Either works.

---

## Phase A — Error decoder union (commit 1)

### Task A1: Add `DecodeErrorEnvelope` helper

**Files:**
- Create: `backend/infra/contract/rag/errors.go`

- [ ] **Step 1: Create the new file with the helper + formatter.**

```go
/*
 * Copyright 2026 coze-dev Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package rag

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// pydanticSyntheticCode is the rag code we report when a pydantic 422
// validation failure is decoded. Rag's pydantic layer does not emit a
// numeric code on the wire; this constant lets MapRagError's existing
// "40001-40009 → InvalidParam" branch pick up validation failures
// without growing a new branch or sentinel argument.
const pydanticSyntheticCode = 40001

// DecodeErrorEnvelope parses a rag error response body into a (code,
// message) pair suitable for MapRagError. It tolerates rag's three real
// envelope shapes (see the R2-C spec §3.1):
//
//   - flat business envelope `{code, message, data, request_id}`
//   - FastAPI HTTPException `{detail: {code, message}}`
//   - pydantic 422 validation `{detail: [{loc, msg, type, ctx}]}`
//
// A body that matches none of them (non-JSON, truncated, empty, or an
// unknown shape) yields (0, ""), letting MapRagError fall through to
// ErrRagUpstreamUnavailableCode — the same outcome as the previous
// hard-coded FastAPI-shape decoder, so no regression on unknown bodies.
//
// Pydantic 422 detail arrays are translated to the synthetic code 40001
// with a formatted message "<dotted-loc>: <msg>"; MapRagError's
// 40001-40009 branch then classifies them as InvalidParam.
func DecodeErrorEnvelope(raw []byte) (code int, message string) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return 0, ""
	}

	// (a) Flat envelope {code, message, data, request_id}.
	var flat struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &flat); err == nil && (flat.Code != 0 || flat.Message != "") {
		return flat.Code, flat.Message
	}

	// (b) FastAPI HTTPException {detail: {code, message}}.
	var fastapi struct {
		Detail struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"detail"`
	}
	if err := json.Unmarshal(raw, &fastapi); err == nil && (fastapi.Detail.Code != 0 || fastapi.Detail.Message != "") {
		return fastapi.Detail.Code, fastapi.Detail.Message
	}

	// (c) Pydantic 422 {detail: [{loc, msg, ...}]}.
	var pydantic struct {
		Detail []struct {
			Loc []any  `json:"loc"`
			Msg string `json:"msg"`
		} `json:"detail"`
	}
	if err := json.Unmarshal(raw, &pydantic); err == nil && len(pydantic.Detail) > 0 {
		d := pydantic.Detail[0]
		return pydanticSyntheticCode, formatPydanticDetail(d.Loc, d.Msg)
	}

	return 0, ""
}

// formatPydanticDetail renders a pydantic error entry as "<dotted-loc>: <msg>".
//
//	loc=["body","query_image","image_base64"], msg="field required"
//	  → "body.query_image.image_base64: field required"
//
// loc entries can be strings or integers (array indices); both are
// stringified via fmt.Sprintf("%v", ...) which produces the natural
// representation without quoting.
func formatPydanticDetail(loc []any, msg string) string {
	parts := make([]string, 0, len(loc))
	for _, p := range loc {
		parts = append(parts, fmt.Sprintf("%v", p))
	}
	return strings.Join(parts, ".") + ": " + msg
}
```

- [ ] **Step 2: Compile-check.**

Run: `cd /Users/liuxinyu/workspace/coze-studio/backend && GOTOOLCHAIN=go1.24.0 go build ./infra/contract/rag/...`
Expected: clean build.

- [ ] **Step 3: Do not commit yet.** Phase A commits at the end of Task A6.

---

### Task A2: Add unit tests for `DecodeErrorEnvelope`

**Files:**
- Create: `backend/infra/contract/rag/errors_test.go`

- [ ] **Step 1: Create the test file with a table-driven test.**

```go
/*
 * Copyright 2026 coze-dev Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package rag

import "testing"

func TestDecodeErrorEnvelope(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantCode    int
		wantMessage string
	}{
		{
			name:        "flat envelope with code and message",
			raw:         `{"code":50001,"message":"model not found","data":null,"request_id":"r1"}`,
			wantCode:    50001,
			wantMessage: "model not found",
		},
		{
			name:        "flat envelope with only message",
			raw:         `{"message":"plain text error"}`,
			wantCode:    0,
			wantMessage: "plain text error",
		},
		{
			name:        "FastAPI HTTPException",
			raw:         `{"detail":{"code":40001,"message":"X-Tenant-Id required"}}`,
			wantCode:    40001,
			wantMessage: "X-Tenant-Id required",
		},
		{
			name:        "pydantic 422 single entry",
			raw:         `{"detail":[{"loc":["body","x"],"msg":"missing","type":"value_error.missing"}]}`,
			wantCode:    40001,
			wantMessage: "body.x: missing",
		},
		{
			name:        "pydantic 422 nested loc with integer index",
			raw:         `{"detail":[{"loc":["body","items",0,"name"],"msg":"field required"}]}`,
			wantCode:    40001,
			wantMessage: "body.items.0.name: field required",
		},
		{
			name:        "pydantic 422 takes first entry only",
			raw:         `{"detail":[{"loc":["a"],"msg":"first"},{"loc":["b"],"msg":"second"}]}`,
			wantCode:    40001,
			wantMessage: "a: first",
		},
		{
			name:        "empty body",
			raw:         ``,
			wantCode:    0,
			wantMessage: "",
		},
		{
			name:        "whitespace-only body",
			raw:         `   `,
			wantCode:    0,
			wantMessage: "",
		},
		{
			name:        "non-JSON body",
			raw:         `<html><body>502 Bad Gateway</body></html>`,
			wantCode:    0,
			wantMessage: "",
		},
		{
			name:        "JSON with no recognised fields",
			raw:         `{"foo":"bar"}`,
			wantCode:    0,
			wantMessage: "",
		},
		{
			name:        "flat envelope with code=0 and empty message falls through",
			raw:         `{"code":0,"message":""}`,
			wantCode:    0,
			wantMessage: "",
		},
		{
			name:        "pydantic 422 with empty detail array falls through",
			raw:         `{"detail":[]}`,
			wantCode:    0,
			wantMessage: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCode, gotMessage := DecodeErrorEnvelope([]byte(tt.raw))
			if gotCode != tt.wantCode {
				t.Errorf("code = %d, want %d", gotCode, tt.wantCode)
			}
			if gotMessage != tt.wantMessage {
				t.Errorf("message = %q, want %q", gotMessage, tt.wantMessage)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./infra/contract/rag/... -v`
Expected: all 12 sub-tests PASS.

- [ ] **Step 3: Do not commit yet.** Continues into Task A3.

---

### Task A3: Rewire `client.go` to use `DecodeErrorEnvelope`

**Files:**
- Modify: `backend/infra/rag/client.go:159-166` (doOnce error path)
- Modify: `backend/infra/rag/client.go:237-241` (doMultipart error path)

- [ ] **Step 1: Update `doOnce`'s error decode (lines 159-166).**

Find:

```go
	if resp.StatusCode >= 400 {
		var errBody contract.ErrorBody
		// Best-effort decode: if the upstream returns non-JSON the error body
		// will be zero-valued and MapRagError will fall through to the
		// upstream-unavailable bucket, which is what we want.
		_ = json.Unmarshal(raw, &errBody)
		return MapRagError(resp.StatusCode, errBody.Detail.Code, errBody.Detail.Message)
	}
```

Replace with:

```go
	if resp.StatusCode >= 400 {
		// DecodeErrorEnvelope tolerates rag's three real envelope shapes (flat,
		// FastAPI HTTPException, pydantic 422). A non-JSON or unknown body
		// returns (0, ""), letting MapRagError fall through to the
		// upstream-unavailable bucket — same outcome as the previous decoder
		// for unknown bodies, but with correct classification for the shapes
		// rag actually emits.
		code, msg := contract.DecodeErrorEnvelope(raw)
		return MapRagError(resp.StatusCode, code, msg)
	}
```

- [ ] **Step 2: Update `doMultipart`'s error decode (lines 237-241).**

Find:

```go
	if resp.StatusCode >= 400 {
		var errBody contract.ErrorBody
		_ = json.Unmarshal(raw, &errBody)
		return MapRagError(resp.StatusCode, errBody.Detail.Code, errBody.Detail.Message)
	}
```

Replace with:

```go
	if resp.StatusCode >= 400 {
		// See doOnce for the decoder rationale; multipart's response path is
		// identical once we have the raw body bytes.
		code, msg := contract.DecodeErrorEnvelope(raw)
		return MapRagError(resp.StatusCode, code, msg)
	}
```

- [ ] **Step 3: Build-check.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./infra/rag/...`
Expected: clean build. `ErrorBody` and `ErrorDetail` are no longer referenced in this package — they only existed for these two call sites.

- [ ] **Step 4: Do not commit yet.** Continues into Task A4.

---

### Task A4: Delete obsolete `ErrorBody` and `ErrorDetail` types

**Files:**
- Modify: `backend/infra/contract/rag/types.go:227-239` (delete ErrorBody + ErrorDetail block)

- [ ] **Step 1: Verify no remaining callers.**

Run: `grep -rn "ErrorBody\|ErrorDetail" backend/infra/ backend/domain/ backend/application/ 2>/dev/null | grep -v "_test.go"`
Expected: empty output (only matches are inside the about-to-be-deleted block, and possibly inside a comment in workflow code that doesn't reference the rag types).

If a match shows up outside `types.go` and outside test files, STOP — the deletion would break a caller. Re-dispatch with the additional caller list.

- [ ] **Step 2: Delete the block.**

Find at `types.go:227-239` (or near; line numbers may have shifted after R2-B):

```go
// ErrorBody matches FastAPI's default HTTPException envelope:
//
//	{"detail": {"code": int, "message": str}}
//
// MapRagError unwraps Detail before classifying.
type ErrorBody struct {
	Detail ErrorDetail `json:"detail"`
}

type ErrorDetail struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
```

Delete the entire block (the comment block and both struct declarations). Leave the surrounding `types.go` content untouched.

- [ ] **Step 3: Build-check.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./...`
Expected: clean build for the entire backend module.

- [ ] **Step 4: Do not commit yet.** Continues into Task A5.

---

### Task A5: Add httptest tests for end-to-end decoder integration

**Files:**
- Modify: `backend/infra/rag/client_test.go` — append two new test functions

- [ ] **Step 1: Append `TestClient_DecodesFlatEnvelopeError` and `TestClient_DecodesPydantic422AsInvalidParam`.**

Place after the last existing test in `client_test.go` (use Read to find the file's last line first). The file already imports the packages needed; you may need `errors` and the `errorx` package — add to the import block if absent.

```go
// TestClient_DecodesFlatEnvelopeError verifies that rag's flat error envelope
// {code, message, data, request_id} flows through the new DecodeErrorEnvelope
// path and reaches MapRagError with the correct rag code and message. A 5xx
// classifies as ErrRagUpstreamUnavailableCode (default branch) — the new
// decoder doesn't change that, only ensures the rag code and message are
// preserved in the error's msg extra for debugging.
func TestClient_DecodesFlatEnvelopeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":       50001,
			"message":    "model not found",
			"data":       nil,
			"request_id": "r1",
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	_, err := c.ListModelProviders(context.Background(), "t1")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	var se errorx.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("expected errorx.StatusError, got %T: %v", err, err)
	}
	if se.Code() != errno.ErrRagUpstreamUnavailableCode {
		t.Errorf("Code() = %d, want %d (ErrRagUpstreamUnavailableCode)", se.Code(), errno.ErrRagUpstreamUnavailableCode)
	}
	// The rag code and message should be preserved in the msg extra for
	// debugging — the previous decoder lost both.
	msgExtra := se.Extra()["msg"]
	if !strings.Contains(msgExtra, "50001") {
		t.Errorf("msg extra = %q, want it to contain rag code 50001", msgExtra)
	}
	if !strings.Contains(msgExtra, "model not found") {
		t.Errorf("msg extra = %q, want it to contain rag message", msgExtra)
	}
}

// TestClient_DecodesPydantic422AsInvalidParam verifies that rag's pydantic
// validation 422 envelope (detail-as-array) is now correctly classified as
// ErrKnowledgeInvalidParamCode instead of being silently treated as upstream-
// unavailable. This is the leverage fix R2-C delivers — every endpoint's
// validation errors become diagnosable.
func TestClient_DecodesPydantic422AsInvalidParam(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"detail": []map[string]any{
				{
					"loc":  []any{"body", "kb_ids"},
					"msg":  "field required",
					"type": "value_error.missing",
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	_, err := c.CreateKB(context.Background(), "t1", &contract.CreateKBRequest{Name: "x"})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	var se errorx.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("expected errorx.StatusError, got %T: %v", err, err)
	}
	if se.Code() != errno.ErrKnowledgeInvalidParamCode {
		t.Errorf("Code() = %d, want %d (ErrKnowledgeInvalidParamCode)", se.Code(), errno.ErrKnowledgeInvalidParamCode)
	}
	msgExtra := se.Extra()["msg"]
	if !strings.Contains(msgExtra, "body.kb_ids") {
		t.Errorf("msg extra = %q, want it to contain formatted loc path 'body.kb_ids'", msgExtra)
	}
	if !strings.Contains(msgExtra, "field required") {
		t.Errorf("msg extra = %q, want it to contain pydantic msg 'field required'", msgExtra)
	}
}
```

- [ ] **Step 2: Check imports.**

The two new tests use `errors`, `strings`, `time`, `context`, `http`, `httptest`, `json`, `ragconf`, `contract`, `errorx`, `errno`. The first six and `ragconf`/`contract` are already imported by other tests in this file. Verify `errors`, `strings`, `github.com/coze-dev/coze-studio/backend/pkg/errorx`, and `github.com/coze-dev/coze-studio/backend/types/errno` are in the import block; add any missing ones.

Run a probe: `cd backend && GOTOOLCHAIN=go1.24.0 go vet ./infra/rag/` to catch any missing import.

- [ ] **Step 3: Run the new tests.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./infra/rag/ -run "TestClient_Decodes" -v`
Expected: both tests PASS.

- [ ] **Step 4: Run the full package test sweep.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./infra/contract/rag/... ./infra/rag/... ./domain/knowledge/service/ragimpl/... ./application/knowledge/...`
Expected: all PASS.

- [ ] **Step 5: Do not commit yet.** Continues into Task A6.

---

### Task A6: Commit Phase A

**Files:** (no edits; commit only)

- [ ] **Step 1: Inspect what changed.**

Run: `git status` from `/Users/liuxinyu/workspace/coze-studio`. Confirm:
- New: `backend/infra/contract/rag/errors.go`
- New: `backend/infra/contract/rag/errors_test.go`
- Modified: `backend/infra/contract/rag/types.go` (ErrorBody/ErrorDetail deleted)
- Modified: `backend/infra/rag/client.go` (two error decode sites rewired)
- Modified: `backend/infra/rag/client_test.go` (two new tests appended)

- [ ] **Step 2: Commit.**

```bash
git add backend/infra/contract/rag/errors.go \
        backend/infra/contract/rag/errors_test.go \
        backend/infra/contract/rag/types.go \
        backend/infra/rag/client.go \
        backend/infra/rag/client_test.go
git commit -m "$(cat <<'EOF'
refactor(rag): union-friendly error envelope decoder

Rag emits three real error envelope shapes today: flat business envelope
{code, message, data, request_id}, FastAPI HTTPException {detail:{code,
message}}, and pydantic 422 validation {detail:[{loc,msg,...}]}. Coze's
ErrorBody was hard-coded to the FastAPI shape — a shape rag does not
emit on either business or validation errors. Result: every rag error
silently decoded to zero values and MapRagError classified everything
as ErrRagUpstreamUnavailableCode, with no rag code or message preserved.
The 2026-05-13 smoke wall ("http=500 rag=0 msg=") was a direct symptom.

Adds contract.DecodeErrorEnvelope(raw) → (code, message) that tries the
three shapes in order; pydantic 422 detail arrays synthesize code 40001
so they slot into MapRagError's existing InvalidParam branch without
requiring an "is-pydantic" sentinel. doOnce and doMultipart both swap
their inline decode for the helper. ErrorBody/ErrorDetail types are
deleted — they had no callers outside the rewired client paths.

httptest tests exercise the flat-envelope (5xx → UpstreamUnavailable
with rag code/message preserved) and pydantic 422 (→ InvalidParam) paths
end-to-end through the client.
EOF
)"
```

---

## Phase B — Retrieve.query_image object shape (commit 2)

### Task B1: Add `QueryImage` struct and change `RetrieveRequest.QueryImage` type

**Files:**
- Modify: `backend/infra/contract/rag/types.go` — add `QueryImage` struct; change `RetrieveRequest.QueryImage` field type

- [ ] **Step 1: Add the `QueryImage` struct above `RetrieveRequest`.**

Find `RetrieveRequest` (around line 193 after Phase A's deletions shifted line numbers — use Read to confirm). Insert the new struct immediately before it, after `FusionPolicy`:

```go
// QueryImage mirrors rag's ImageQueryDTO (app/api/schemas/retrieval.py).
// Rag's RetrievalRequest enforces extra="forbid" at the top level, so a bare
// base64 string in the query_image field is rejected with HTTP 422. Use this
// object type to carry either an inline base64 payload or a reference to a
// previously-uploaded image in the object store; at least one of the two
// fields must be non-empty (rag's _has_query_input enforces it; coze does not
// pre-validate, letting pydantic 422 surface back via DecodeErrorEnvelope).
type QueryImage struct {
	ImageBase64 string `json:"image_base64,omitempty"`
	ImageRef    string `json:"image_ref,omitempty"`
}
```

- [ ] **Step 2: Change `RetrieveRequest.QueryImage` from `*string` to `*QueryImage`.**

Find at `types.go` (around line 196 pre-Phase-A; line number shifted after Task A4's deletion):

```go
	QueryImage       *string        `json:"query_image,omitempty"`
```

Replace with:

```go
	QueryImage       *QueryImage    `json:"query_image,omitempty"`
```

- [ ] **Step 3: Verify no callers reference the old type.**

Run: `grep -rn "RetrieveRequest{" backend/ 2>/dev/null | grep -v "_test.go"`
Expected: no production caller constructs `RetrieveRequest` with `QueryImage`. (Pre-flight grep already confirmed this; this step is the executor's belt-and-suspenders.)

Also: `grep -rn "QueryImage" backend/ 2>/dev/null | grep -v "_test.go"` — expect only the field definition and the new struct definition in `types.go`.

If any caller appears that passes `*string` for QueryImage, STOP and report — the rename would break the build.

- [ ] **Step 4: Build-check.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./...`
Expected: clean build.

- [ ] **Step 5: Do not commit yet.** Continues into Task B2.

---

### Task B2: Add httptest wire-shape lock for `Retrieve` with `QueryImage`

**Files:**
- Modify: `backend/infra/rag/client_test.go` — append `TestRetrieve_QueryImageObject`

- [ ] **Step 1: Append the new test after the Phase A tests.**

```go
// TestRetrieve_QueryImageObject locks rag's RetrievalRequest.query_image wire
// shape after the 0e1f49b audit. Before R2-C, coze sent a bare base64 string
// here and rag's StrictBaseModel(extra="forbid") rejected it with HTTP 422.
// This test posts a QueryImage{ImageBase64: ...} and asserts the wire body
// has the nested object form `"query_image":{"image_base64":"..."}`.
func TestRetrieve_QueryImageObject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/api/v1/retrieval") {
			t.Errorf("path = %s, want suffix /api/v1/retrieval", r.URL.Path)
		}
		if got := r.Header.Get("X-Tenant-Id"); got != "t1" {
			t.Errorf("X-Tenant-Id = %q, want %q", got, "t1")
		}

		// Decode the request body into a structurally-flexible map so we can
		// assert on the nested query_image shape exactly. Decoding into
		// contract.RetrieveRequest would tautologically succeed because the
		// type itself is what we are locking; map-decode keeps the assertion
		// honest at the wire level.
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("decode body: %v; body=%s", err, body)
		}
		qi, ok := got["query_image"].(map[string]any)
		if !ok {
			t.Fatalf("query_image is not an object, body=%s", body)
		}
		if qi["image_base64"] != "abc" {
			t.Errorf("query_image.image_base64 = %v, want \"abc\"", qi["image_base64"])
		}
		// image_ref omitted on the wire (omitempty); the map should not have it.
		if _, present := qi["image_ref"]; present {
			t.Errorf("query_image.image_ref should be omitted when empty, got it present")
		}

		_, _ = w.Write(envelopeBody(t, contract.RetrieveResponse{
			Items: []contract.RetrieveHit{{ChunkID: "c1", Score: 0.9}},
		}))
	}))
	t.Cleanup(srv.Close)

	c := New(ragconf.Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
	out, err := c.Retrieve(context.Background(), "t1", &contract.RetrieveRequest{
		KBIDs:      []string{"kb-1"},
		QueryImage: &contract.QueryImage{ImageBase64: "abc"},
		QueryMode:  "image_input",
	})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(out.Items) != 1 || out.Items[0].ChunkID != "c1" {
		t.Errorf("decoded response = %+v, want one hit chunk_id=c1", out)
	}
}
```

- [ ] **Step 2: Check imports.**

Test uses `io` in addition to the existing imports. Verify `io` is in the import block of `client_test.go`; if absent, add it. (R2-A's `TestCreateDocument_Multipart` already uses `io.ReadAll`, so it's likely there.)

- [ ] **Step 3: Run the test.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./infra/rag/ -run TestRetrieve_QueryImageObject -v`
Expected: PASS.

- [ ] **Step 4: Run the full package sweep.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go test ./infra/contract/rag/... ./infra/rag/... ./domain/knowledge/service/ragimpl/... ./application/knowledge/...`
Expected: all PASS. `TestRetrieve` (the original Retrieve happy-path test from pre-R2-C) still passes — it constructs a RetrieveRequest without setting QueryImage, so the pointer change is invisible to it.

- [ ] **Step 5: Do not commit yet.** Continues into Task B3.

---

### Task B3: Commit Phase B

**Files:** (no edits; commit only)

- [ ] **Step 1: Inspect what changed.**

Run: `git status` from `/Users/liuxinyu/workspace/coze-studio`. Confirm:
- Modified: `backend/infra/contract/rag/types.go` (QueryImage struct added; RetrieveRequest.QueryImage type changed)
- Modified: `backend/infra/rag/client_test.go` (one new test appended)

- [ ] **Step 2: Commit.**

```bash
git add backend/infra/contract/rag/types.go \
        backend/infra/rag/client_test.go
git commit -m "$(cat <<'EOF'
refactor(rag): switch Retrieve.query_image to object shape

Rag's RetrievalRequest is StrictBaseModel(extra="forbid") and query_image
now expects an ImageQueryDTO object with {image_base64, image_ref}, not
a bare base64 string. Coze's existing *string field was rejected at rag's
pydantic layer with HTTP 422 — pure-text retrieval kept working because
the field is omitempty.

Adds a QueryImage struct mirroring ImageQueryDTO; RetrieveRequest.QueryImage
becomes *QueryImage. No production caller constructs query_image today
(image search isn't wired through the UI yet); this lands the contract
ahead of the use case. httptest locks the nested-object wire shape so a
future shape drift fails a unit test rather than a smoke.
EOF
)"
```

---

## Phase D — Verification

### Task D1: Full backend test sweep + go vet

**Files:** (no edits; verification only)

- [ ] **Step 1: Test sweep.**

```bash
cd /Users/liuxinyu/workspace/coze-studio/backend && GOTOOLCHAIN=go1.24.0 go test \
  ./infra/contract/rag/... \
  ./infra/rag/... \
  ./domain/knowledge/service/ragimpl/... \
  ./application/knowledge/...
```

Expected: PASS for all packages.

- [ ] **Step 2: Full backend build.**

Run: `cd backend && GOTOOLCHAIN=go1.24.0 go build ./...`
Expected: clean build.

- [ ] **Step 3: Vet.**

```bash
cd backend && GOTOOLCHAIN=go1.24.0 go vet \
  ./infra/contract/rag/... \
  ./infra/rag/... \
  ./domain/knowledge/service/ragimpl/... \
  ./application/knowledge/...
```

Expected: clean.

- [ ] **Step 4: If vet flags anything, fix it as a new commit (do not amend).**

---

### Task D2: Smoke (optional, exploratory)

Per spec §8.4, this is exploratory rather than required. The leverage fix (error decoder) doesn't have a clean UI smoke trigger — the existing flows mostly go through happy paths. The QueryImage fix has no UI exposure yet.

If you want to confirm the error decoder works end-to-end:

- [ ] **Step 1: Trigger a deliberate pydantic 422 from rag.**

With the stacks up from R2-B's smoke, try uploading a doc with an invalid file_type via curl directly (bypassing the UI):

```bash
# Get the rag_kb_id of an existing rag-backed KB:
docker exec coze-mysql mysql -uroot -proot opencoze -sN -e "SELECT rag_kb_id FROM rag_kb_mapping LIMIT 1;"

# Force rag's pydantic to reject by sending a missing required form field:
curl -i -X POST -H "X-Tenant-Id: coze" \
  -F "file_type=txt" \
  -F "source_modality=text_source" \
  http://localhost:8000/api/v1/knowledgebases/<rag_kb_id>/documents
# Expected: 422 with pydantic detail array mentioning the missing `file` part.
```

Then trigger the same path through coze (e.g., simulate a malformed CreateDocument from a test harness) and observe `/tmp/coze-server.log`:

- Before R2-C: `[Error] ... rag service unavailable: http=422 rag=0 msg=`
- After R2-C: `[Error] ... rag invalid param: http=422 rag=40001 msg=body.file: field required` (or similar)

- [ ] **Step 2: Confirm classification surfaces a different errno.**

If you have a debugger or test harness, assert the returned error has `Code() == errno.ErrKnowledgeInvalidParamCode` (105000000) instead of `ErrRagUpstreamUnavailableCode` (105100002).

- [ ] **Step 3: No commit.** Smoke does not change tracked files.

---

## Out of scope (do not address in this plan)

- **R2-D**: new endpoints (`/capabilities`, `POST .../retry`, `/document-parameter-schemas`) and the wizard rework that consumes them.
- **R2-E**: broader httptest scaffolding for the rest of the rag client surface; extending `rag-contract-check` to body schemas.
- **`MapRagError` refactor**: keeping the existing branch structure is a deliberate spec choice (§2 non-goal). Pydantic 422 piggy-backs on the 40001 branch via the synthetic code; a future refactor that distinguishes "rag's pydantic rejected" from "rag's own 40001 business error" can land as its own change.
- **New errno values** (e.g. `ErrRagValidationFailed`) — deferred until application-layer callers actually need to distinguish.
- **Frontend changes** — none. The image-search UI is not yet exposed; this slice prepares the contract for a future entry point.
- **`Retrieve.QueryMode` semantics** — coze currently sends `"text_input"` or `"image_input"` here; R2-C doesn't change that. Future capability filtering belongs to R2-D.

---

## Self-review checklist (filled in)

1. **Spec coverage** — every section in the spec has a corresponding task:
   - §3.2 QueryImage struct + RetrieveRequest field change → Task B1
   - §3.3 DecodeErrorEnvelope helper → Task A1
   - §4 architecture flows (error path + image-search) → Tasks A1, A3, B1
   - §4.3 file table → Tasks A1-A4 + B1-B2 (file-by-file)
   - §5.1 implementation outline → Task A1 Step 1 (code block matches the spec's pseudocode)
   - §5.2 formatPydanticDetail → Task A1 Step 1 (helper included)
   - §5.3 QueryImage struct → Task B1 Step 1
   - §5.4 caller updates → Tasks A3 (client.go rewire) + B1 (RetrieveRequest field)
   - §7 error handling table → Task A5 (httptest verifies flat-envelope and pydantic-422 rows)
   - §8.1 unit tests for DecodeErrorEnvelope → Task A2 (table has all enumerated cases)
   - §8.2 httptest tests → Task A5 (flat envelope + pydantic 422) + Task B2 (QueryImage)
   - §8.3 existing tests stay green → Task D1
   - §8.4 smoke → Task D2 (marked exploratory)

2. **Placeholders** — none. Task B1 Step 2 has a defensive grep for production callers (none expected, but the step ensures the executor verifies before the rename). Task D2 is explicitly marked optional.

3. **Type consistency** — `QueryImage{ImageBase64, ImageRef}` matches between Task B1 (definition) and Task B2 (test usage). `DecodeErrorEnvelope(raw) (code int, message string)` signature matches between Task A1 (definition), Task A2 (unit tests), Task A3 (client.go call sites), and Task A5 (httptest assertions). `pydanticSyntheticCode = 40001` is consistent with the synthetic-code rationale and aligns with `MapRagError`'s existing 40001-40009 → InvalidParam branch.
