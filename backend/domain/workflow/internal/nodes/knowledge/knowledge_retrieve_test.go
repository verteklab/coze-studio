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
