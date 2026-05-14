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
