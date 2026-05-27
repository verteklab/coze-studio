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
	"context"
	"testing"

	"github.com/coze-dev/coze-studio/backend/domain/workflow/entity/vo"
)

// newDatasetParam helper builds a vo.Param of type "list" whose content is []any.
// Mirrors the shape produced by the frontend data-transformer for datasetList and
// the new documentIDs block (R2-D-fe-Wizard).
func newDatasetParam(name string, schemaType vo.VariableType, ids []any) *vo.Param {
	return &vo.Param{
		Name: name,
		Input: &vo.BlockInput{
			Type:   vo.VariableTypeList,
			Schema: map[string]any{"type": schemaType},
			Value: &vo.BlockInputValue{
				Type:    "literal",
				Content: ids,
			},
		},
	}
}

// newScalarParam helper builds a vo.Param of a scalar (non-list) shape. Pass
// nil for `content` to model the "UI left blank" case — the frontend emits the
// param with an absent Content key, which round-trips through JSON as a nil
// any. See TestAdapt_BlankTopKDoesNotEmitZero for why that matters.
func newScalarParam(name string, t vo.VariableType, content any) *vo.Param {
	return &vo.Param{
		Name: name,
		Input: &vo.BlockInput{
			Type: t,
			Value: &vo.BlockInputValue{
				Type:    "literal",
				Content: content,
			},
		},
	}
}

// TestAdapt_BlankTopKDoesNotEmitZero locks in that a topK DatasetParam whose
// Content is nil (the wire shape produced by the UI when the user leaves the
// topK input empty) does NOT cause RetrievalStrategy.TopK to be set to 0.
//
// Why this matters: cast.ToInt64E(nil) returns (0, nil), and the upstream code
// previously assigned that 0 to TopK unconditionally. Rag's retrieval validator
// requires top_k > 0 (retrieval_validator.py:_validate_numeric_limits — see
// rag/app/policy/validators/retrieval_validator.py); sending top_k=0 was
// rejected as `rag 40004: 检索参数无效` and the workflow knowledge-retrieve
// node failed with that message. Reproduced via:
//
//	curl -H X-Tenant-Id: ... POST /api/v1/retrieval -d '{..., "top_k": 0}'
//	-> { "code": 40004, "message": "检索参数无效" }
//
// Regression guard for that incident.
func TestAdapt_BlankTopKDoesNotEmitZero(t *testing.T) {
	cfg := &RetrieveConfig{}
	node := &vo.Node{
		ID:   "n1",
		Type: "knowledge-retrieve",
		Data: &vo.Data{
			Meta: &vo.NodeMetaFE{Title: "test-retrieve"},
			Inputs: &vo.Inputs{
				InputParameters: []*vo.Param{},
				Knowledge: &vo.Knowledge{
					DatasetParam: []*vo.Param{
						newDatasetParam("datasetList", vo.VariableTypeString, []any{int64(1)}),
						newScalarParam("topK", vo.VariableTypeInteger, nil),
					},
				},
			},
		},
	}

	if _, err := cfg.Adapt(context.Background(), node); err != nil {
		t.Fatalf("Adapt returned error: %v", err)
	}

	if cfg.RetrievalStrategy == nil {
		t.Fatalf("RetrievalStrategy is nil after Adapt")
	}
	if cfg.RetrievalStrategy.TopK != nil {
		t.Errorf("RetrievalStrategy.TopK = %v, want nil (blank UI input must not become top_k=0 on the wire)", *cfg.RetrievalStrategy.TopK)
	}
}

// TestAdapt_ExplicitZeroTopKAlsoDropped covers the rare case where the UI sent
// an explicit 0 (e.g. the user typed "0" then submitted). Rag rejects top_k=0
// the same way as the blank-input case, so the cleanest semantic is "drop
// non-positive values, let rag apply its own default" rather than propagate
// a value that's guaranteed to 40004.
func TestAdapt_ExplicitZeroTopKAlsoDropped(t *testing.T) {
	cfg := &RetrieveConfig{}
	node := &vo.Node{
		ID:   "n1",
		Type: "knowledge-retrieve",
		Data: &vo.Data{
			Meta: &vo.NodeMetaFE{Title: "test-retrieve"},
			Inputs: &vo.Inputs{
				InputParameters: []*vo.Param{},
				Knowledge: &vo.Knowledge{
					DatasetParam: []*vo.Param{
						newDatasetParam("datasetList", vo.VariableTypeString, []any{int64(1)}),
						newScalarParam("topK", vo.VariableTypeInteger, int64(0)),
					},
				},
			},
		},
	}

	if _, err := cfg.Adapt(context.Background(), node); err != nil {
		t.Fatalf("Adapt returned error: %v", err)
	}

	if cfg.RetrievalStrategy == nil {
		t.Fatalf("RetrievalStrategy is nil after Adapt")
	}
	if cfg.RetrievalStrategy.TopK != nil {
		t.Errorf("RetrievalStrategy.TopK = %v, want nil (top_k=0 must not reach rag)", *cfg.RetrievalStrategy.TopK)
	}
}

// TestAdapt_PositiveTopKIsKept verifies the positive path still works — the
// drop is gated on non-positive only, not "drop topK entirely".
func TestAdapt_PositiveTopKIsKept(t *testing.T) {
	cfg := &RetrieveConfig{}
	node := &vo.Node{
		ID:   "n1",
		Type: "knowledge-retrieve",
		Data: &vo.Data{
			Meta: &vo.NodeMetaFE{Title: "test-retrieve"},
			Inputs: &vo.Inputs{
				InputParameters: []*vo.Param{},
				Knowledge: &vo.Knowledge{
					DatasetParam: []*vo.Param{
						newDatasetParam("datasetList", vo.VariableTypeString, []any{int64(1)}),
						newScalarParam("topK", vo.VariableTypeInteger, int64(5)),
					},
				},
			},
		},
	}

	if _, err := cfg.Adapt(context.Background(), node); err != nil {
		t.Fatalf("Adapt returned error: %v", err)
	}

	if cfg.RetrievalStrategy == nil || cfg.RetrievalStrategy.TopK == nil {
		t.Fatalf("RetrievalStrategy.TopK is nil, want pointer to 5")
	}
	if *cfg.RetrievalStrategy.TopK != 5 {
		t.Errorf("RetrievalStrategy.TopK = %d, want 5", *cfg.RetrievalStrategy.TopK)
	}
}

// newMapParam helper builds a vo.Param of object shape.
func newMapParam(name string, content map[string]any) *vo.Param {
	return &vo.Param{
		Name: name,
		Input: &vo.BlockInput{
			Type: vo.VariableTypeObject,
			Value: &vo.BlockInputValue{
				Type:    "literal",
				Content: content,
			},
		},
	}
}

// TestAdapt_ParsesNewQueryStrategy verifies the 4 new boolean params
// (rewrite / expansion / multiQuery / enableRerank) land on the
// matching RetrievalStrategy fields.
func TestAdapt_ParsesNewQueryStrategy(t *testing.T) {
	cfg := &RetrieveConfig{}
	node := &vo.Node{
		ID:   "n1",
		Type: "knowledge-retrieve",
		Data: &vo.Data{
			Meta: &vo.NodeMetaFE{Title: "test-retrieve"},
			Inputs: &vo.Inputs{
				InputParameters: []*vo.Param{},
				Knowledge: &vo.Knowledge{
					DatasetParam: []*vo.Param{
						newDatasetParam("datasetList", vo.VariableTypeString, []any{int64(1)}),
						newScalarParam("rewrite", vo.VariableTypeBoolean, true),
						newScalarParam("expansion", vo.VariableTypeBoolean, false),
						newScalarParam("multiQuery", vo.VariableTypeBoolean, true),
						newScalarParam("enableRerank", vo.VariableTypeBoolean, true),
					},
				},
			},
		},
	}

	if _, err := cfg.Adapt(context.Background(), node); err != nil {
		t.Fatalf("Adapt error: %v", err)
	}
	if cfg.RetrievalStrategy == nil {
		t.Fatalf("RetrievalStrategy is nil")
	}
	if !cfg.RetrievalStrategy.Rewrite {
		t.Errorf("Rewrite = false, want true")
	}
	if cfg.RetrievalStrategy.Expansion {
		t.Errorf("Expansion = true, want false")
	}
	if !cfg.RetrievalStrategy.MultiQuery {
		t.Errorf("MultiQuery = false, want true")
	}
	if !cfg.RetrievalStrategy.EnableRerank {
		t.Errorf("EnableRerank = false, want true")
	}
}

// TestAdapt_ParsesFiltersRetrieversTargetChunkTypes verifies map and
// list params hydrate the corresponding RetrievalStrategy fields.
func TestAdapt_ParsesFiltersRetrieversTargetChunkTypes(t *testing.T) {
	cfg := &RetrieveConfig{}
	node := &vo.Node{
		ID:   "n1",
		Type: "knowledge-retrieve",
		Data: &vo.Data{
			Meta: &vo.NodeMetaFE{Title: "test-retrieve"},
			Inputs: &vo.Inputs{
				InputParameters: []*vo.Param{},
				Knowledge: &vo.Knowledge{
					DatasetParam: []*vo.Param{
						newDatasetParam("datasetList", vo.VariableTypeString, []any{int64(1)}),
						newDatasetParam("targetChunkTypes", vo.VariableTypeString, []any{"text_chunk"}),
						newDatasetParam("retrievers", vo.VariableTypeString, []any{"dense", "bm25"}),
						newMapParam("filters", map[string]any{"tag": "guides", "year": int64(2026)}),
					},
				},
			},
		},
	}

	if _, err := cfg.Adapt(context.Background(), node); err != nil {
		t.Fatalf("Adapt error: %v", err)
	}
	if got := cfg.RetrievalStrategy.TargetChunkTypes; len(got) != 1 || got[0] != "text_chunk" {
		t.Errorf("TargetChunkTypes = %v, want [text_chunk]", got)
	}
	if got := cfg.RetrievalStrategy.Retrievers; len(got) != 2 || got[0] != "dense" || got[1] != "bm25" {
		t.Errorf("Retrievers = %v, want [dense bm25]", got)
	}
	if got := cfg.RetrievalStrategy.Filters; got["tag"] != "guides" || got["year"] != int64(2026) {
		t.Errorf("Filters = %+v, want {tag:guides year:2026}", got)
	}
}

// TestAdapt_ParsesQueryImage verifies image_base64 / image_ref hydrate
// the QueryImage; an empty payload leaves the field nil.
func TestAdapt_ParsesQueryImage(t *testing.T) {
	cfg := &RetrieveConfig{}
	node := &vo.Node{
		ID:   "n1",
		Type: "knowledge-retrieve",
		Data: &vo.Data{
			Meta: &vo.NodeMetaFE{Title: "test-retrieve"},
			Inputs: &vo.Inputs{
				InputParameters: []*vo.Param{},
				Knowledge: &vo.Knowledge{
					DatasetParam: []*vo.Param{
						newDatasetParam("datasetList", vo.VariableTypeString, []any{int64(1)}),
						newMapParam("queryImage", map[string]any{"image_ref": "ref-1"}),
					},
				},
			},
		},
	}

	if _, err := cfg.Adapt(context.Background(), node); err != nil {
		t.Fatalf("Adapt error: %v", err)
	}
	if cfg.RetrievalStrategy.QueryImage == nil {
		t.Fatalf("QueryImage is nil")
	}
	if cfg.RetrievalStrategy.QueryImage.ImageRef != "ref-1" {
		t.Errorf("ImageRef = %q, want ref-1", cfg.RetrievalStrategy.QueryImage.ImageRef)
	}
}

// TestAdapt_RejectsInvalidQueryMode verifies a non-enum queryMode
// returns an error rather than silently propagating to rag.
func TestAdapt_RejectsInvalidQueryMode(t *testing.T) {
	cfg := &RetrieveConfig{}
	node := &vo.Node{
		ID:   "n1",
		Type: "knowledge-retrieve",
		Data: &vo.Data{
			Meta: &vo.NodeMetaFE{Title: "test-retrieve"},
			Inputs: &vo.Inputs{
				InputParameters: []*vo.Param{},
				Knowledge: &vo.Knowledge{
					DatasetParam: []*vo.Param{
						newDatasetParam("datasetList", vo.VariableTypeString, []any{int64(1)}),
						newScalarParam("queryMode", vo.VariableTypeString, "garbage"),
					},
				},
			},
		},
	}

	_, err := cfg.Adapt(context.Background(), node)
	if err == nil {
		t.Fatalf("Adapt returned no error; want validation error for queryMode=garbage")
	}
}

// TestAdapt_IgnoresLegacyParams locks in that the new Adapt silently
// drops legacy param names (useRewrite/useRerank/useNl2sql/
// isPersonalOnly/minScore/documentIDs) so old workflow JSON loads
// without error.
func TestAdapt_IgnoresLegacyParams(t *testing.T) {
	cfg := &RetrieveConfig{}
	node := &vo.Node{
		ID:   "n1",
		Type: "knowledge-retrieve",
		Data: &vo.Data{
			Meta: &vo.NodeMetaFE{Title: "test-retrieve"},
			Inputs: &vo.Inputs{
				InputParameters: []*vo.Param{},
				Knowledge: &vo.Knowledge{
					DatasetParam: []*vo.Param{
						newDatasetParam("datasetList", vo.VariableTypeString, []any{int64(1)}),
						newScalarParam("useRewrite", vo.VariableTypeBoolean, true),
						newScalarParam("useRerank", vo.VariableTypeBoolean, true),
						newScalarParam("useNl2sql", vo.VariableTypeBoolean, true),
						newScalarParam("isPersonalOnly", vo.VariableTypeBoolean, true),
						newScalarParam("minScore", vo.VariableTypeFloat, 0.7),
						newDatasetParam("documentIDs", vo.VariableTypeInteger, []any{int64(1), int64(2)}),
					},
				},
			},
		},
	}

	if _, err := cfg.Adapt(context.Background(), node); err != nil {
		t.Fatalf("Adapt returned error for legacy params: %v", err)
	}
	if cfg.RetrievalStrategy == nil {
		t.Fatalf("RetrievalStrategy is nil")
	}
	// None of the new fields should be set from legacy params.
	if cfg.RetrievalStrategy.Rewrite || cfg.RetrievalStrategy.EnableRerank {
		t.Errorf("legacy useRewrite/useRerank leaked into new fields")
	}
}
