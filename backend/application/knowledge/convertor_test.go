/*
 * Copyright 2025 coze-dev Authors
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

package knowledge

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// resolveDatasetBackend is the per-record decision used inside
// batchConvertKnowledgeEntity2Model: "rag" if a KB id appears in the
// rag_kb_mapping set, "legacy" otherwise. We test it directly so the
// branch logic and pointer-aliasing safety are covered without the
// 30-method service.Knowledge mock the full convertor would require.
func TestResolveDatasetBackend_RagWhenMapped(t *testing.T) {
	ragMapped := map[int64]struct{}{12345: {}}
	got := resolveDatasetBackend(12345, ragMapped)
	require.NotNil(t, got)
	require.Equal(t, "rag", *got)
}

func TestResolveDatasetBackend_LegacyWhenUnmapped(t *testing.T) {
	ragMapped := map[int64]struct{}{12345: {}}
	got := resolveDatasetBackend(67890, ragMapped)
	require.NotNil(t, got)
	require.Equal(t, "legacy", *got)
}

func TestResolveDatasetBackend_LegacyOnEmptySet(t *testing.T) {
	// Legacy-backend deployments hit this path: mappingRepo is nil so the
	// caller passes an empty map. Every id must resolve to "legacy".
	got := resolveDatasetBackend(42, map[int64]struct{}{})
	require.NotNil(t, got)
	require.Equal(t, "legacy", *got)
}

func TestResolveDatasetBackend_LegacyOnNilSet(t *testing.T) {
	// Defensive: a nil set must behave identically to an empty one. Reads
	// on a nil map are legal in Go but we exercise the path explicitly.
	got := resolveDatasetBackend(42, nil)
	require.NotNil(t, got)
	require.Equal(t, "legacy", *got)
}

func TestResolveDatasetBackend_FreshPointerPerCall(t *testing.T) {
	// The convertor sets Dataset.Backend on each record in a loop; if the
	// helper returned a shared *string, all records would alias and a later
	// mutation would poison earlier ones. Pin the no-aliasing contract.
	ragMapped := map[int64]struct{}{1: {}, 2: {}}
	a := resolveDatasetBackend(1, ragMapped)
	b := resolveDatasetBackend(2, ragMapped)
	c := resolveDatasetBackend(3, ragMapped)
	require.NotSame(t, a, b)
	require.NotSame(t, a, c)
	require.NotSame(t, b, c)
	require.Equal(t, "rag", *a)
	require.Equal(t, "rag", *b)
	require.Equal(t, "legacy", *c)
}
