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
	"strings"
	"testing"
)

func TestMapRagError_InvalidParam(t *testing.T) {
	err := MapRagError(400, 40001, "missing field")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "missing field") {
		t.Fatalf("err missing message: %v", err)
	}
}

func TestMapRagError_KBNotFound(t *testing.T) {
	err := MapRagError(404, 40401, "kb not found")
	if !strings.Contains(err.Error(), "kb not found") {
		t.Fatalf("got %v", err)
	}
}

func TestMapRagError_DocNotFound(t *testing.T) {
	err := MapRagError(404, 40402, "doc not found")
	if !strings.Contains(err.Error(), "doc not found") {
		t.Fatalf("got %v", err)
	}
}

func TestMapRagError_Conflict(t *testing.T) {
	err := MapRagError(409, 40901, "duplicate name")
	if !strings.Contains(err.Error(), "duplicate name") {
		t.Fatalf("got %v", err)
	}
}

func TestMapRagError_Fallback(t *testing.T) {
	err := MapRagError(500, 50001, "internal")
	if !strings.Contains(err.Error(), "rag=50001") {
		t.Fatalf("got %v", err)
	}
}
