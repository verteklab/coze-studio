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

// TestAdapt_ParsesDocumentIDs verifies that RetrieveConfig.Adapt extracts the
// documentIDs param (JSON list of numbers) into RetrieveConfig.DocumentIDs.
//
// The frontend serializes selected documents as:
//
//	{ name: "documentIDs", input: { type: "list", schema: { type: "number" },
//	  value: { type: "literal", content: [101, 202, 303] } } }
//
// After JSON round-trip, numeric items arrive as float64 / int64 (depending on
// decoder); cast.ToInt64E handles both. Test the mixed path here.
func TestAdapt_ParsesDocumentIDs(t *testing.T) {
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
						// datasetList must be index 0 -- handler asserts that.
						newDatasetParam("datasetList", vo.VariableTypeString, []any{int64(1), int64(2)}),
						newDatasetParam("documentIDs", vo.VariableTypeInteger, []any{
							int64(101), float64(202), 303,
						}),
					},
				},
			},
		},
	}

	if _, err := cfg.Adapt(context.Background(), node); err != nil {
		t.Fatalf("Adapt returned error: %v", err)
	}

	if got, want := len(cfg.DocumentIDs), 3; got != want {
		t.Fatalf("DocumentIDs length = %d, want %d (cfg=%+v)", got, want, cfg.DocumentIDs)
	}
	for i, want := range []int64{101, 202, 303} {
		if cfg.DocumentIDs[i] != want {
			t.Errorf("DocumentIDs[%d] = %d, want %d", i, cfg.DocumentIDs[i], want)
		}
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

// TestAdapt_NoDocumentIDsKeepsNil ensures the parse loop is opt-in: when the
// param is absent (the common case -- "all docs in KB"), DocumentIDs stays nil
// so the rag /retrieval call doesn't get an empty list that would filter out
// every result.
func TestAdapt_NoDocumentIDsKeepsNil(t *testing.T) {
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
					},
				},
			},
		},
	}

	if _, err := cfg.Adapt(context.Background(), node); err != nil {
		t.Fatalf("Adapt returned error: %v", err)
	}

	if cfg.DocumentIDs != nil {
		t.Errorf("DocumentIDs = %+v, want nil", cfg.DocumentIDs)
	}
}
