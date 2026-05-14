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
