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

package ragimpl

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	knowledgeModel "github.com/coze-dev/coze-studio/backend/crossdomain/knowledge/model"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/entity"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/service"
	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
	"github.com/coze-dev/coze-studio/backend/pkg/errorx"
	"github.com/coze-dev/coze-studio/backend/types/errno"
)

// TestCreateDocument_InsertsMapping asserts that a successful rag CreateDocument
// is followed by a mapping insert carrying rag_doc_id, last_task_id, and the
// caller's KB / creator info. The returned Document has its Status translated
// via RagStatusToEntity (rag "pending" -> coze DocumentStatusInit).
func TestCreateDocument_InsertsMapping(t *testing.T) {
	fc := &fakeClient{
		createDocFunc: func(_, _ string, _ *contract.CreateDocumentRequest) (*contract.CreateDocumentResponse, error) {
			return &contract.CreateDocumentResponse{DocID: "rag-doc-A", TaskID: "task-A", Status: "pending"}, nil
		},
	}
	i := newTestImpl(t, fc, 7777)
	// Seed a KB mapping so KBByCozeID succeeds.
	require.NoError(t, i.mapping.InsertKB(context.Background(), 100, "rag-kb-100", "icon", 0, 5, 0, knowledgeModel.DocumentTypeText))

	resp, err := i.CreateDocument(context.Background(), &service.CreateDocumentRequest{
		Documents: []*entity.Document{{
			Info:        knowledgeModel.Info{Name: "doc.txt", CreatorID: 5},
			KnowledgeID: 100,
			Type:        knowledgeModel.DocumentTypeText,
			URI:         "s3://x/y",
		}},
	})
	require.NoError(t, err)
	require.Len(t, resp.Documents, 1)
	require.Equal(t, int64(7777), resp.Documents[0].ID)
	require.Equal(t, entity.DocumentStatusInit, resp.Documents[0].Status)

	// rag.CreateDocument was called with tenant from resolver (header arg) +
	// correct modality. Tenant is no longer in the request body.
	require.Equal(t, "rag-kb-100", fc.createDocKBID)
	require.Equal(t, "test-tenant", fc.createDocTenant)
	require.Equal(t, "text_source", fc.createDocReq.SourceModality)
	// A document with no ParsingStrategy must not populate any of the new
	// Phase 1 passthrough fields, so rag's per-schema defaults apply.
	require.Nil(t, fc.createDocReq.EnableOCR)
	require.Nil(t, fc.createDocReq.EnableImageEmbedding)
	require.Empty(t, fc.createDocReq.DocumentOptions)

	// Mapping row inserted with rag_doc_id and last_task_id.
	got, err := i.mapping.DocByCozeID(context.Background(), 7777)
	require.NoError(t, err)
	require.Equal(t, "rag-doc-A", got.RagDocID)
	require.Equal(t, "task-A", got.LastTaskID)
	require.Equal(t, int64(100), got.KBID)
	require.Equal(t, int64(5), got.CreatorID)
}

// TestCreateDocument_StrategyPassthrough verifies the Phase 1 mapping from
// coze's ParsingStrategy to rag's per-document fields. Subcases pin the
// schema-routing rules: enable_ocr / enable_image_embedding only travel on
// modalities that declare them, and PDF + OCR-intent gets routed to the
// scanned schema so those fields land somewhere valid.
func TestCreateDocument_StrategyPassthrough(t *testing.T) {
	t.Run("pdf with OCR+ExtractImage routes to scanned and emits both bools", func(t *testing.T) {
		// Rag's text-side schemas (text/markdown/pdf_text/docx) have NO
		// enable_ocr or enable_image_embedding fields, so when the user asks
		// for OCR or image extraction on a PDF the only valid landing is
		// scanned_document_source. extract_tables does NOT travel — the
		// scanned schema has no table-extraction toggle.
		fc := &fakeClient{
			createDocFunc: func(_, _ string, _ *contract.CreateDocumentRequest) (*contract.CreateDocumentResponse, error) {
				return &contract.CreateDocumentResponse{DocID: "rag-doc-pdf", TaskID: "task-pdf", Status: "pending"}, nil
			},
		}
		i := newTestImpl(t, fc, 8001)
		require.NoError(t, i.mapping.InsertKB(context.Background(), 200, "rag-kb-200", "icon", 0, 5, 0, knowledgeModel.DocumentTypeText))

		_, err := i.CreateDocument(context.Background(), &service.CreateDocumentRequest{
			Documents: []*entity.Document{{
				Info:          knowledgeModel.Info{Name: "report.pdf", CreatorID: 5},
				KnowledgeID:   200,
				Type:          knowledgeModel.DocumentTypeText,
				FileExtension: "pdf",
				URI:           "s3://x/y",
				ParsingStrategy: &entity.ParsingStrategy{
					ImageOCR:     true,
					ExtractImage: true,
					ExtractTable: true,
				},
			}},
		})
		require.NoError(t, err)
		require.Equal(t, "scanned_document_source", fc.createDocReq.SourceModality)
		require.NotNil(t, fc.createDocReq.EnableOCR)
		require.True(t, *fc.createDocReq.EnableOCR)
		require.NotNil(t, fc.createDocReq.EnableImageEmbedding)
		require.True(t, *fc.createDocReq.EnableImageEmbedding)
		require.Empty(t, fc.createDocReq.DocumentOptions,
			"scanned_document schema has no extract_tables knob — document_options must stay empty")
	})

	t.Run("pdf with ExtractImage but no OCR stays on text_source (no scanned promotion)", func(t *testing.T) {
		// Regression guard for the 2026-05-19 incident: ExtractImage alone
		// used to promote PDFs to scanned_document_source, but rag's scanned
		// schema requires `ocr_model_id` whenever it's chosen (enable_ocr
		// default=True there), so ExtractImage-only uploads 40001'd. Now we
		// only promote when ImageOCR is explicitly true.
		fc := &fakeClient{
			createDocFunc: func(_, _ string, _ *contract.CreateDocumentRequest) (*contract.CreateDocumentResponse, error) {
				return &contract.CreateDocumentResponse{DocID: "rag-doc-pdf-img", TaskID: "task-pdf-img", Status: "pending"}, nil
			},
		}
		i := newTestImpl(t, fc, 8000)
		require.NoError(t, i.mapping.InsertKB(context.Background(), 199, "rag-kb-199", "icon", 0, 5, 0, knowledgeModel.DocumentTypeText))

		_, err := i.CreateDocument(context.Background(), &service.CreateDocumentRequest{
			Documents: []*entity.Document{{
				Info:          knowledgeModel.Info{Name: "img.pdf", CreatorID: 5},
				KnowledgeID:   199,
				Type:          knowledgeModel.DocumentTypeText,
				FileExtension: "pdf",
				URI:           "s3://x/y",
				ParsingStrategy: &entity.ParsingStrategy{
					ImageOCR:     false,
					ExtractImage: true,
				},
			}},
		})
		require.NoError(t, err)
		require.Equal(t, "text_source", fc.createDocReq.SourceModality,
			"ExtractImage alone must not flip modality — scanned would require ocr_model_id")
		require.Nil(t, fc.createDocReq.EnableOCR)
		require.Nil(t, fc.createDocReq.EnableImageEmbedding,
			"text_source schema has no enable_image_embedding; ExtractImage dropped silently")
	})

	t.Run("pdf with ExtractTable only stays on text_source and emits extract_tables", func(t *testing.T) {
		// No OCR / image intent -> modality stays text_source, which is the
		// only schema that actually has extract_tables.
		fc := &fakeClient{
			createDocFunc: func(_, _ string, _ *contract.CreateDocumentRequest) (*contract.CreateDocumentResponse, error) {
				return &contract.CreateDocumentResponse{DocID: "rag-doc-pdf-tbl", TaskID: "task-pdf-tbl", Status: "pending"}, nil
			},
		}
		i := newTestImpl(t, fc, 8002)
		require.NoError(t, i.mapping.InsertKB(context.Background(), 201, "rag-kb-201", "icon", 0, 5, 0, knowledgeModel.DocumentTypeText))

		_, err := i.CreateDocument(context.Background(), &service.CreateDocumentRequest{
			Documents: []*entity.Document{{
				Info:          knowledgeModel.Info{Name: "tbl.pdf", CreatorID: 5},
				KnowledgeID:   201,
				Type:          knowledgeModel.DocumentTypeText,
				FileExtension: "pdf",
				URI:           "s3://x/y",
				ParsingStrategy: &entity.ParsingStrategy{
					ExtractTable: true,
				},
			}},
		})
		require.NoError(t, err)
		require.Equal(t, "text_source", fc.createDocReq.SourceModality)
		require.Nil(t, fc.createDocReq.EnableOCR)
		require.Nil(t, fc.createDocReq.EnableImageEmbedding)
		require.JSONEq(t, `{"extract_tables":true}`, fc.createDocReq.DocumentOptions)
	})

	t.Run("docx with OCR drops the flag (no scanned-docx schema)", func(t *testing.T) {
		// docx_document has no enable_ocr / enable_image_embedding, and there's
		// no scanned_docx schema to escape to. We drop the flags silently
		// rather than 40001 on rag.
		fc := &fakeClient{
			createDocFunc: func(_, _ string, _ *contract.CreateDocumentRequest) (*contract.CreateDocumentResponse, error) {
				return &contract.CreateDocumentResponse{DocID: "rag-doc-docx", TaskID: "task-docx", Status: "pending"}, nil
			},
		}
		i := newTestImpl(t, fc, 8003)
		require.NoError(t, i.mapping.InsertKB(context.Background(), 202, "rag-kb-202", "icon", 0, 5, 0, knowledgeModel.DocumentTypeText))

		_, err := i.CreateDocument(context.Background(), &service.CreateDocumentRequest{
			Documents: []*entity.Document{{
				Info:          knowledgeModel.Info{Name: "spec.docx", CreatorID: 5},
				KnowledgeID:   202,
				Type:          knowledgeModel.DocumentTypeText,
				FileExtension: "docx",
				URI:           "s3://x/y",
				ParsingStrategy: &entity.ParsingStrategy{
					ImageOCR:     true,
					ExtractImage: true,
					ExtractTable: true,
				},
			}},
		})
		require.NoError(t, err)
		require.Equal(t, "text_source", fc.createDocReq.SourceModality)
		require.Nil(t, fc.createDocReq.EnableOCR)
		require.Nil(t, fc.createDocReq.EnableImageEmbedding)
		require.JSONEq(t, `{"extract_tables":true}`, fc.createDocReq.DocumentOptions)
	})

	t.Run("image-typed doc with OCR keeps image_source and emits both bools", func(t *testing.T) {
		fc := &fakeClient{
			createDocFunc: func(_, _ string, _ *contract.CreateDocumentRequest) (*contract.CreateDocumentResponse, error) {
				return &contract.CreateDocumentResponse{DocID: "rag-doc-img", TaskID: "task-img", Status: "pending"}, nil
			},
		}
		i := newTestImpl(t, fc, 8004)
		require.NoError(t, i.mapping.InsertKB(context.Background(), 203, "rag-kb-203", "icon", 0, 5, 0, knowledgeModel.DocumentTypeText))

		_, err := i.CreateDocument(context.Background(), &service.CreateDocumentRequest{
			Documents: []*entity.Document{{
				Info:          knowledgeModel.Info{Name: "diagram.png", CreatorID: 5},
				KnowledgeID:   203,
				Type:          knowledgeModel.DocumentTypeImage,
				FileExtension: "png",
				URI:           "s3://x/y",
				ParsingStrategy: &entity.ParsingStrategy{
					ImageOCR:     true,
					ExtractImage: true,
				},
			}},
		})
		require.NoError(t, err)
		require.Equal(t, "image_source", fc.createDocReq.SourceModality)
		require.NotNil(t, fc.createDocReq.EnableOCR)
		require.True(t, *fc.createDocReq.EnableOCR)
		require.NotNil(t, fc.createDocReq.EnableImageEmbedding)
		require.True(t, *fc.createDocReq.EnableImageEmbedding)
	})

	t.Run("txt with ExtractTable=true must not emit extract_tables", func(t *testing.T) {
		// Sanity check: txt's text_document schema has no extract_tables.
		fc := &fakeClient{
			createDocFunc: func(_, _ string, _ *contract.CreateDocumentRequest) (*contract.CreateDocumentResponse, error) {
				return &contract.CreateDocumentResponse{DocID: "rag-doc-txt", TaskID: "task-txt", Status: "pending"}, nil
			},
		}
		i := newTestImpl(t, fc, 8005)
		require.NoError(t, i.mapping.InsertKB(context.Background(), 204, "rag-kb-204", "icon", 0, 5, 0, knowledgeModel.DocumentTypeText))

		_, err := i.CreateDocument(context.Background(), &service.CreateDocumentRequest{
			Documents: []*entity.Document{{
				Info:          knowledgeModel.Info{Name: "notes.txt", CreatorID: 5},
				KnowledgeID:   204,
				Type:          knowledgeModel.DocumentTypeText,
				FileExtension: "txt",
				URI:           "s3://x/y",
				ParsingStrategy: &entity.ParsingStrategy{
					ExtractTable: true,
				},
			}},
		})
		require.NoError(t, err)
		require.Equal(t, "text_source", fc.createDocReq.SourceModality)
		require.Empty(t, fc.createDocReq.DocumentOptions,
			"txt schema has no extract_tables knob — document_options must stay empty")
	})

	t.Run("DocumentOptions override modality and forward cleaned blob", func(t *testing.T) {
		// Phase 3b: dynamic form sends `_source_modality` reserved key to
		// override the auto-routing, plus per-schema knobs in the rest of
		// the JSON. Backend must strip `_source_modality` before forwarding
		// (rag's pydantic extra=forbid would 422 on an unknown top-level
		// key inside document_options).
		fc := &fakeClient{
			createDocFunc: func(_, _ string, _ *contract.CreateDocumentRequest) (*contract.CreateDocumentResponse, error) {
				return &contract.CreateDocumentResponse{DocID: "rag-doc-dyn", TaskID: "task-dyn", Status: "pending"}, nil
			},
		}
		i := newTestImpl(t, fc, 8100)
		require.NoError(t, i.mapping.InsertKB(context.Background(), 250, "rag-kb-250", "icon", 0, 5, 0, knowledgeModel.DocumentTypeText))

		_, err := i.CreateDocument(context.Background(), &service.CreateDocumentRequest{
			Documents: []*entity.Document{{
				Info:          knowledgeModel.Info{Name: "report.pdf", CreatorID: 5},
				KnowledgeID:   250,
				Type:          knowledgeModel.DocumentTypeText,
				FileExtension: "pdf",
				URI:           "s3://x/y",
				// No typed strategy: dynamic form is the only source of truth.
			}},
			DocumentOptions: `{"_source_modality":"scanned_document_source","ocr_languages":["zh","en"],"split_by_page":true}`,
		})
		require.NoError(t, err)
		require.Equal(t, "scanned_document_source", fc.createDocReq.SourceModality,
			"_source_modality must override the auto-routed modality")
		var forwarded map[string]any
		require.NoError(t, json.Unmarshal([]byte(fc.createDocReq.DocumentOptions), &forwarded))
		require.NotContains(t, forwarded, "_source_modality",
			"reserved key must be stripped before forwarding to rag")
		require.Equal(t, []any{"zh", "en"}, forwarded["ocr_languages"])
		require.Equal(t, true, forwarded["split_by_page"])
	})

	t.Run("DocumentOptions only with reserved key emits empty options", func(t *testing.T) {
		// If the dynamic form only set the modality override and nothing else,
		// document_options after stripping is {} which we MUST send as "" so
		// rag falls back to per-schema defaults instead of seeing an empty
		// object literal.
		fc := &fakeClient{
			createDocFunc: func(_, _ string, _ *contract.CreateDocumentRequest) (*contract.CreateDocumentResponse, error) {
				return &contract.CreateDocumentResponse{DocID: "rag-doc-mod", TaskID: "task-mod", Status: "pending"}, nil
			},
		}
		i := newTestImpl(t, fc, 8101)
		require.NoError(t, i.mapping.InsertKB(context.Background(), 251, "rag-kb-251", "icon", 0, 5, 0, knowledgeModel.DocumentTypeText))

		_, err := i.CreateDocument(context.Background(), &service.CreateDocumentRequest{
			Documents: []*entity.Document{{
				Info:          knowledgeModel.Info{Name: "scan.pdf", CreatorID: 5},
				KnowledgeID:   251,
				Type:          knowledgeModel.DocumentTypeText,
				FileExtension: "pdf",
				URI:           "s3://x/y",
			}},
			DocumentOptions: `{"_source_modality":"scanned_document_source"}`,
		})
		require.NoError(t, err)
		require.Equal(t, "scanned_document_source", fc.createDocReq.SourceModality)
		require.Empty(t, fc.createDocReq.DocumentOptions)
	})

	t.Run("DocumentOptions malformed JSON returns invalid-param error", func(t *testing.T) {
		fc := &fakeClient{
			createDocFunc: func(_, _ string, _ *contract.CreateDocumentRequest) (*contract.CreateDocumentResponse, error) {
				t.Fatal("rag.CreateDocument must not be called on malformed input")
				return nil, nil
			},
		}
		i := newTestImpl(t, fc, 8102)
		require.NoError(t, i.mapping.InsertKB(context.Background(), 252, "rag-kb-252", "icon", 0, 5, 0, knowledgeModel.DocumentTypeText))

		_, err := i.CreateDocument(context.Background(), &service.CreateDocumentRequest{
			Documents: []*entity.Document{{
				Info:          knowledgeModel.Info{Name: "x.pdf", CreatorID: 5},
				KnowledgeID:   252,
				Type:          knowledgeModel.DocumentTypeText,
				FileExtension: "pdf",
				URI:           "s3://x/y",
			}},
			DocumentOptions: `{bad json`,
		})
		require.Error(t, err)
	})

	t.Run("ParsingStrategy with all zero values emits nothing", func(t *testing.T) {
		fc := &fakeClient{
			createDocFunc: func(_, _ string, _ *contract.CreateDocumentRequest) (*contract.CreateDocumentResponse, error) {
				return &contract.CreateDocumentResponse{DocID: "rag-doc-zero", TaskID: "task-zero", Status: "pending"}, nil
			},
		}
		i := newTestImpl(t, fc, 8006)
		require.NoError(t, i.mapping.InsertKB(context.Background(), 205, "rag-kb-205", "icon", 0, 5, 0, knowledgeModel.DocumentTypeText))

		_, err := i.CreateDocument(context.Background(), &service.CreateDocumentRequest{
			Documents: []*entity.Document{{
				Info:            knowledgeModel.Info{Name: "blank.pdf", CreatorID: 5},
				KnowledgeID:     205,
				Type:            knowledgeModel.DocumentTypeText,
				FileExtension:   "pdf",
				URI:             "s3://x/y",
				ParsingStrategy: &entity.ParsingStrategy{}, // all false / zero
			}},
		})
		require.NoError(t, err)
		require.Equal(t, "text_source", fc.createDocReq.SourceModality)
		require.Nil(t, fc.createDocReq.EnableOCR)
		require.Nil(t, fc.createDocReq.EnableImageEmbedding)
		require.Empty(t, fc.createDocReq.DocumentOptions)
	})
}

// TestMGetDocumentProgress_NoMirror verifies two invariants:
//   - rag's task status is translated correctly ("success" -> DocumentStatusEnable)
//   - the mapping row is NOT touched (last_task_id unchanged) -- the mapping
//     table has no status column, and rag is the system of record.
func TestMGetDocumentProgress_NoMirror(t *testing.T) {
	fc := &fakeClient{
		getTaskFunc: func(_, _ string) (*contract.Task, error) {
			return &contract.Task{TaskID: "task-Z", Status: "success"}, nil
		},
	}
	i := newTestImpl(t, fc)
	require.NoError(t, i.mapping.InsertDoc(context.Background(), 4242, "rag-doc-Z", 100, 7, "task-Z", 1700000000, 0, ""))

	resp, err := i.MGetDocumentProgress(context.Background(), &service.MGetDocumentProgressRequest{
		DocumentIDs: []int64{4242},
	})
	require.NoError(t, err)
	require.Len(t, resp.ProgressList, 1)
	require.Equal(t, entity.DocumentStatusEnable, resp.ProgressList[0].Status)
	require.Equal(t, 100, resp.ProgressList[0].Progress)

	// Mapping row was NOT modified -- last_task_id stays exactly what we seeded.
	got, err := i.mapping.DocByCozeID(context.Background(), 4242)
	require.NoError(t, err)
	require.Equal(t, "task-Z", got.LastTaskID, "MGetDocumentProgress must not mirror status to mapping table")
}

// TestMGetDocumentProgress_FilenameSet asserts that when rag's GetTask returns
// a non-nil `filename`, MGetDocumentProgress copies it onto DocumentProgress.Name.
// Without this, the upload-progress UI falls back to rendering raw doc IDs.
func TestMGetDocumentProgress_FilenameSet(t *testing.T) {
	fn := "report-q3.pdf"
	fc := &fakeClient{
		getTaskFunc: func(_, _ string) (*contract.Task, error) {
			return &contract.Task{TaskID: "task-fn-1", Status: "running", Filename: &fn}, nil
		},
	}
	i := newTestImpl(t, fc)
	require.NoError(t, i.mapping.InsertDoc(context.Background(), 4301, "rag-doc-fn-1", 100, 7, "task-fn-1", 1700000000, 0, ""))

	resp, err := i.MGetDocumentProgress(context.Background(), &service.MGetDocumentProgressRequest{
		DocumentIDs: []int64{4301},
	})
	require.NoError(t, err)
	require.Len(t, resp.ProgressList, 1)
	require.Equal(t, "report-q3.pdf", resp.ProgressList[0].Name)
}

// TestMGetDocumentProgress_FilenameNil asserts that when rag returns a nil
// filename pointer (Optional[str] = null on the wire), Name stays empty.
// The frontend's `name || id` fallback covers the rendering.
func TestMGetDocumentProgress_FilenameNil(t *testing.T) {
	fc := &fakeClient{
		getTaskFunc: func(_, _ string) (*contract.Task, error) {
			return &contract.Task{TaskID: "task-fn-nil", Status: "pending", Filename: nil}, nil
		},
	}
	i := newTestImpl(t, fc)
	require.NoError(t, i.mapping.InsertDoc(context.Background(), 4302, "rag-doc-fn-nil", 100, 7, "task-fn-nil", 1700000000, 0, ""))

	resp, err := i.MGetDocumentProgress(context.Background(), &service.MGetDocumentProgressRequest{
		DocumentIDs: []int64{4302},
	})
	require.NoError(t, err)
	require.Len(t, resp.ProgressList, 1)
	require.Equal(t, "", resp.ProgressList[0].Name)
}

// TestMGetDocumentProgress_MixedFilenames covers the realistic upload-batch
// case: a per-doc fakeClient returns a filename for some tasks and a nil for
// others. Each progress entry must carry the right name (or "" for the nil).
func TestMGetDocumentProgress_MixedFilenames(t *testing.T) {
	a := "a.pdf"
	b := "b.pdf"
	filenames := map[string]*string{
		"task-A": &a,
		"task-B": &b,
		"task-C": nil,
	}
	fc := &fakeClient{
		getTaskFunc: func(_, taskID string) (*contract.Task, error) {
			return &contract.Task{TaskID: taskID, Status: "success", Filename: filenames[taskID]}, nil
		},
	}
	i := newTestImpl(t, fc)
	require.NoError(t, i.mapping.InsertDoc(context.Background(), 5001, "rag-doc-A", 100, 7, "task-A", 1700000000, 0, ""))
	require.NoError(t, i.mapping.InsertDoc(context.Background(), 5002, "rag-doc-B", 100, 7, "task-B", 1700000000, 0, ""))
	require.NoError(t, i.mapping.InsertDoc(context.Background(), 5003, "rag-doc-C", 100, 7, "task-C", 1700000000, 0, ""))

	resp, err := i.MGetDocumentProgress(context.Background(), &service.MGetDocumentProgressRequest{
		DocumentIDs: []int64{5001, 5002, 5003},
	})
	require.NoError(t, err)
	require.Len(t, resp.ProgressList, 3)
	byID := map[int64]string{}
	for _, dp := range resp.ProgressList {
		byID[dp.ID] = dp.Name
	}
	require.Equal(t, "a.pdf", byID[5001])
	require.Equal(t, "b.pdf", byID[5002])
	require.Equal(t, "", byID[5003])
}

// TestUpdateDocument_HappyPath_RenameOnly verifies that ragimpl.UpdateDocument
// translates a service.UpdateDocumentRequest{DocumentName: ptr("new.pdf")} to
// a rag POST /documents/{doc_id}/update carrying filename only. The mapping is
// resolved via DocByCozeID + KBByCozeID so the rag-side UUIDs are used; the
// rag response body is discarded (service interface returns nil error on
// success, the frontend will re-fetch via MGetDocument).
func TestUpdateDocument_HappyPath_RenameOnly(t *testing.T) {
	var gotTenant, gotKBID, gotDocID string
	var gotReq *contract.UpdateDocumentRequest
	fc := &fakeClient{
		updateDocFunc: func(tenantID, kbID, docID string, req *contract.UpdateDocumentRequest) (*contract.Document, error) {
			gotTenant, gotKBID, gotDocID, gotReq = tenantID, kbID, docID, req
			return &contract.Document{DocID: docID, Filename: "new.pdf", Status: "ready"}, nil
		},
	}
	i := newTestImpl(t, fc)
	require.NoError(t, i.mapping.InsertKB(context.Background(), 100, "rag-kb-X", "icon", 0, 0, 1700000000, knowledgeModel.DocumentTypeText))
	require.NoError(t, i.mapping.InsertDoc(context.Background(), 500, "rag-doc-Y", 100, 7, "task-1", 1700000000, 0, ""))

	newName := "new.pdf"
	err := i.UpdateDocument(context.Background(), &service.UpdateDocumentRequest{
		DocumentID:   500,
		DocumentName: &newName,
	})
	require.NoError(t, err)

	require.Equal(t, "test-tenant", gotTenant)
	require.Equal(t, "rag-kb-X", gotKBID)
	require.Equal(t, "rag-doc-Y", gotDocID)
	require.NotNil(t, gotReq)
	require.NotNil(t, gotReq.Filename)
	require.Equal(t, "new.pdf", *gotReq.Filename)
	// Other update fields are unset → nil → omitempty drops them on the wire.
	require.Nil(t, gotReq.Tags)
	require.Nil(t, gotReq.Category)
}

// TestUpdateDocument_TableInfo_Rejected asserts that requests carrying a
// non-nil TableInfo are rejected up-front with ErrKnowledgeInvalidParamCode
// and never reach the rag client. Rag's update DTO has no table fields; table
// metadata updates are bucket-A's table-ingestion work, not this slice.
func TestUpdateDocument_TableInfo_Rejected(t *testing.T) {
	called := false
	fc := &fakeClient{
		updateDocFunc: func(_, _, _ string, _ *contract.UpdateDocumentRequest) (*contract.Document, error) {
			called = true
			return nil, nil
		},
	}
	i := newTestImpl(t, fc)
	require.NoError(t, i.mapping.InsertKB(context.Background(), 100, "rag-kb-X", "icon", 0, 0, 1700000000, knowledgeModel.DocumentTypeText))
	require.NoError(t, i.mapping.InsertDoc(context.Background(), 500, "rag-doc-Y", 100, 7, "task-1", 1700000000, 0, ""))

	newName := "new.pdf"
	err := i.UpdateDocument(context.Background(), &service.UpdateDocumentRequest{
		DocumentID:   500,
		DocumentName: &newName,
		TableInfo:    &entity.TableInfo{},
	})
	require.Error(t, err)
	require.False(t, called, "rag client must NOT be called when TableInfo is set")

	var se errorx.StatusError
	require.True(t, errors.As(err, &se), "expected errorx.StatusError, got %T: %v", err, err)
	require.Equal(t, int32(errno.ErrKnowledgeInvalidParamCode), se.Code())
}

// TestUpdateDocument_MappingNotFound asserts that an unknown coze doc id
// surfaces ErrMappingNotFound without calling the rag client.
func TestUpdateDocument_MappingNotFound(t *testing.T) {
	called := false
	fc := &fakeClient{
		updateDocFunc: func(_, _, _ string, _ *contract.UpdateDocumentRequest) (*contract.Document, error) {
			called = true
			return nil, nil
		},
	}
	i := newTestImpl(t, fc)

	newName := "new.pdf"
	err := i.UpdateDocument(context.Background(), &service.UpdateDocumentRequest{
		DocumentID:   999,
		DocumentName: &newName,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrMappingNotFound)
	require.False(t, called, "rag client must NOT be called when mapping is missing")
}

// TestUpdateDocument_RagError_Propagated verifies that an error from the rag
// client surfaces unwrapped to the caller (no swallowing, no re-classification).
func TestUpdateDocument_RagError_Propagated(t *testing.T) {
	want := errors.New("rag down")
	fc := &fakeClient{
		updateDocFunc: func(_, _, _ string, _ *contract.UpdateDocumentRequest) (*contract.Document, error) {
			return nil, want
		},
	}
	i := newTestImpl(t, fc)
	require.NoError(t, i.mapping.InsertKB(context.Background(), 100, "rag-kb-X", "icon", 0, 0, 1700000000, knowledgeModel.DocumentTypeText))
	require.NoError(t, i.mapping.InsertDoc(context.Background(), 500, "rag-doc-Y", 100, 7, "task-1", 1700000000, 0, ""))

	newName := "new.pdf"
	err := i.UpdateDocument(context.Background(), &service.UpdateDocumentRequest{
		DocumentID:   500,
		DocumentName: &newName,
	})
	require.ErrorIs(t, err, want)
}

// TestRagimpl_RetryDocument verifies that ragimpl.RetryDocument resolves the
// coze doc id to its rag UUID and the owning KB's rag UUID via the mapping
// table, forwards the call to the rag client, and bumps the mapping's
// last_task_id so MGetDocumentProgress follows the retry's new task.
func TestRagimpl_RetryDocument(t *testing.T) {
	var gotTenant, gotKBID, gotDocID string
	fc := &fakeClient{
		retryDocumentFunc: func(tenantID, kbID, docID string) (*contract.CreateDocumentResponse, error) {
			gotTenant, gotKBID, gotDocID = tenantID, kbID, docID
			return &contract.CreateDocumentResponse{
				DocID: docID, TaskID: "task-retry-9", Status: "pending",
			}, nil
		},
	}
	i := newTestImpl(t, fc)

	// Wire mapping rows: coze KB 100 → rag UUID "rag-kb-X";
	// coze doc 500 → rag UUID "rag-doc-Y" in KB 100 with last_task_id="task-old-1".
	require.NoError(t, i.mapping.InsertKB(context.Background(), 100, "rag-kb-X", "icon", 0, 0, 1700000000, knowledgeModel.DocumentTypeText))
	require.NoError(t, i.mapping.InsertDoc(context.Background(), 500, "rag-doc-Y", 100, 7, "task-old-1", 1700000000, 0, ""))

	resp, err := i.RetryDocument(context.Background(), &service.RetryDocumentRequest{DocumentID: 500})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Document)
	require.Equal(t, int64(500), resp.Document.ID)
	require.Equal(t, entity.DocumentStatusInit, resp.Document.Status) // rag "pending" → Init
	require.Equal(t, int64(100), resp.Document.KnowledgeID)

	// Rag client received the rag-side UUIDs:
	require.Equal(t, "test-tenant", gotTenant)
	require.Equal(t, "rag-kb-X", gotKBID)
	require.Equal(t, "rag-doc-Y", gotDocID)

	// CRITICAL: mapping's last_task_id was bumped to the new task.
	dm, err := i.mapping.DocByCozeID(context.Background(), 500)
	require.NoError(t, err)
	require.Equal(t, "task-retry-9", dm.LastTaskID, "mapping must be updated so MGetDocumentProgress polls the new task")
}

// TestRagimpl_RetryDocument_MissingDocMapping verifies that a missing doc
// mapping row surfaces ErrMappingNotFound without calling the rag client.
func TestRagimpl_RetryDocument_MissingDocMapping(t *testing.T) {
	called := false
	fc := &fakeClient{
		retryDocumentFunc: func(_, _, _ string) (*contract.CreateDocumentResponse, error) {
			called = true
			return nil, nil
		},
	}
	i := newTestImpl(t, fc)

	_, err := i.RetryDocument(context.Background(), &service.RetryDocumentRequest{DocumentID: 999})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrMappingNotFound)
	require.False(t, called, "rag client should NOT be called when mapping is missing")
}

// --- buildDocumentEntity URL population ---------------------------------------

// TestBuildDocumentEntity_PopulatesURLFromMappingImageURL verifies that
// buildDocumentEntity copies DocMapping.ImageURL into entity.Document.URL.
// This is the read-side half of the image_url column: Task 8 writes it at
// upload time; Task 9 surfaces it here so the detail page can render thumbnails.
func TestBuildDocumentEntity_PopulatesURLFromMappingImageURL(t *testing.T) {
	dm := &DocMapping{
		CozeID:    9001,
		RagDocID:  "rag-doc-img",
		KBID:      100,
		CreatorID: 7,
		ImageURL:  "https://minio.local/foo.png",
	}
	rd := &contract.Document{
		DocID:    "rag-doc-img",
		Filename: "foo.png",
		Status:   "ready",
	}
	got := buildDocumentEntity(dm, rd)
	require.Equal(t, "https://minio.local/foo.png", got.URL,
		"entity.Document.URL must be populated from DocMapping.ImageURL")
}

// TestBuildDocumentEntity_LeavesURLEmptyWhenMappingImageURLEmpty verifies that
// a DocMapping with no image_url (pre-Task-8 uploads or non-image docs) results
// in an empty entity.Document.URL rather than a nil-pointer panic or garbage.
func TestBuildDocumentEntity_LeavesURLEmptyWhenMappingImageURLEmpty(t *testing.T) {
	dm := &DocMapping{
		CozeID:   9002,
		RagDocID: "rag-doc-txt",
		KBID:     100,
		ImageURL: "", // non-image doc or pre-Task-8 upload
	}
	rd := &contract.Document{
		DocID:    "rag-doc-txt",
		Filename: "notes.txt",
		Status:   "ready",
	}
	got := buildDocumentEntity(dm, rd)
	require.Equal(t, "", got.URL,
		"entity.Document.URL must be empty when DocMapping.ImageURL is empty")
}
