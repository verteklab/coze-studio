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
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	knowledgeModel "github.com/coze-dev/coze-studio/backend/crossdomain/knowledge/model"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/entity"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/service"
	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
)

// seedKBAndDoc inserts an active KB + Doc mapping pair. Most slice tests need
// the same skeleton (one KB, one Doc under it) so this keeps them readable.
func seedKBAndDoc(t *testing.T, i *Impl, cozeKBID int64, ragKBID string, cozeDocID int64, ragDocID string) {
	t.Helper()
	require.NoError(t, i.mapping.InsertKB(context.Background(), cozeKBID, ragKBID, "", 0, 0, 0, knowledgeModel.DocumentTypeText))
	require.NoError(t, i.mapping.InsertDoc(context.Background(), cozeDocID, ragDocID, cozeKBID, 0, "", 0, 0, ""))
}

// strPtr is a tiny helper to build *string literals inline.
func strPtr(s string) *string { return &s }

// --- CreateSlice ----------------------------------------------------------

func TestCreateSlice_TextHappyPath(t *testing.T) {
	fc := &fakeClient{
		createChunkFunc: func(_, _, _ string, _ *contract.CreateChunkRequest) (*contract.Chunk, error) {
			return &contract.Chunk{ChunkID: "rag-chunk-1", DocID: "rag-doc-1", KBID: "rag-kb-1", ChunkType: "text_chunk", Status: "ready"}, nil
		},
	}
	i := newTestImpl(t, fc, 5001)
	ctx := context.Background()
	seedKBAndDoc(t, i, 100, "rag-kb-1", 200, "rag-doc-1")

	resp, err := i.CreateSlice(ctx, &service.CreateSliceRequest{
		DocumentID: 200,
		CreatorID:  7,
		RawContent: []*knowledgeModel.SliceContent{{Type: knowledgeModel.SliceContentTypeText, Text: strPtr("hello")}},
	})
	require.NoError(t, err)
	require.Equal(t, int64(5001), resp.SliceID)

	// Mapping was inserted.
	cm, err := i.mapping.ChunkByCozeID(ctx, 5001)
	require.NoError(t, err)
	require.Equal(t, "rag-chunk-1", cm.RagChunkID)
	require.Equal(t, "rag-doc-1", cm.RagDocID)
	require.Equal(t, int64(200), cm.CozeDocID)

	// rag request fields were forwarded correctly.
	require.Equal(t, "test-tenant", fc.createChunkTenant)
	require.Equal(t, "rag-kb-1", fc.createChunkKBID)
	require.Equal(t, "rag-doc-1", fc.createChunkDocID)
	require.Equal(t, "text_chunk", fc.createChunkReq.ChunkType)
	require.Equal(t, "hello", fc.createChunkReq.Content)
}

func TestCreateSlice_MultiTextJoined(t *testing.T) {
	fc := &fakeClient{}
	i := newTestImpl(t, fc, 5002)
	ctx := context.Background()
	seedKBAndDoc(t, i, 100, "rag-kb-1", 200, "rag-doc-1")

	_, err := i.CreateSlice(ctx, &service.CreateSliceRequest{
		DocumentID: 200,
		RawContent: []*knowledgeModel.SliceContent{
			{Type: knowledgeModel.SliceContentTypeText, Text: strPtr("a")},
			{Type: knowledgeModel.SliceContentTypeText, Text: strPtr("b")},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "a\nb", fc.createChunkReq.Content, "multiple text entries should join with newline")
}

func TestCreateSlice_ImageChunk(t *testing.T) {
	fc := &fakeClient{
		createChunkFunc: func(_, _, _ string, _ *contract.CreateChunkRequest) (*contract.Chunk, error) {
			return &contract.Chunk{ChunkID: "rag-img-1", ChunkType: "image_chunk", Status: "ready"}, nil
		},
	}
	i := newTestImpl(t, fc, 6001)
	ctx := context.Background()
	seedKBAndDoc(t, i, 100, "rag-kb-1", 200, "rag-doc-1")

	_, err := i.CreateSlice(ctx, &service.CreateSliceRequest{
		DocumentID: 200,
		RawContent: []*knowledgeModel.SliceContent{
			{Image: &knowledgeModel.SliceImage{URI: "minio://b/img.png", OCR: true, OCRText: strPtr("alt")}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "image_chunk", fc.createChunkReq.ChunkType)
	require.NotNil(t, fc.createChunkReq.Image)
	require.Equal(t, "minio://b/img.png", fc.createChunkReq.Image.ImageRef)
	require.Equal(t, "alt", fc.createChunkReq.Image.OCRText)
	require.True(t, fc.createChunkReq.Image.OCRUsed)
}

func TestCreateSlice_RejectsTable(t *testing.T) {
	fc := &fakeClient{}
	i := newTestImpl(t, fc, 7001)
	ctx := context.Background()
	seedKBAndDoc(t, i, 100, "rag-kb-1", 200, "rag-doc-1")

	_, err := i.CreateSlice(ctx, &service.CreateSliceRequest{
		DocumentID: 200,
		RawContent: []*knowledgeModel.SliceContent{
			{Type: knowledgeModel.SliceContentTypeTable},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "table chunk")
	require.Nil(t, fc.createChunkReq, "rag must not be called when input contains a table chunk")
}

func TestCreateSlice_RejectsEmptyRawContent(t *testing.T) {
	fc := &fakeClient{}
	i := newTestImpl(t, fc, 7002)
	ctx := context.Background()
	seedKBAndDoc(t, i, 100, "rag-kb-1", 200, "rag-doc-1")

	_, err := i.CreateSlice(ctx, &service.CreateSliceRequest{DocumentID: 200, RawContent: nil})
	require.Error(t, err)
	require.Nil(t, fc.createChunkReq)
}

func TestCreateSlice_RagFailure_NoMappingWritten(t *testing.T) {
	fc := &fakeClient{
		createChunkFunc: func(_, _, _ string, _ *contract.CreateChunkRequest) (*contract.Chunk, error) {
			return nil, errors.New("rag 50001 embed failed")
		},
	}
	i := newTestImpl(t, fc, 8001)
	ctx := context.Background()
	seedKBAndDoc(t, i, 100, "rag-kb-1", 200, "rag-doc-1")

	_, err := i.CreateSlice(ctx, &service.CreateSliceRequest{
		DocumentID: 200,
		RawContent: []*knowledgeModel.SliceContent{{Type: knowledgeModel.SliceContentTypeText, Text: strPtr("hi")}},
	})
	require.Error(t, err)
	// No mapping row should have been allocated.
	_, err = i.mapping.ChunkByCozeID(ctx, 8001)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrMappingNotFound)
}

func TestCreateSlice_DocMappingMissing(t *testing.T) {
	fc := &fakeClient{}
	i := newTestImpl(t, fc, 9001)
	ctx := context.Background()
	// KB exists, doc doesn't.
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-1", "", 0, 0, 0, knowledgeModel.DocumentTypeText))

	_, err := i.CreateSlice(ctx, &service.CreateSliceRequest{
		DocumentID: 999,
		RawContent: []*knowledgeModel.SliceContent{{Type: knowledgeModel.SliceContentTypeText, Text: strPtr("hi")}},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrMappingNotFound)
	require.Nil(t, fc.createChunkReq, "rag must not be called when doc mapping is missing")
}

// --- UpdateSlice ----------------------------------------------------------

func TestUpdateSlice_HappyPath(t *testing.T) {
	fc := &fakeClient{}
	i := newTestImpl(t, fc)
	ctx := context.Background()
	seedKBAndDoc(t, i, 100, "rag-kb-1", 200, "rag-doc-1")
	require.NoError(t, i.mapping.ChunkInsert(ctx, &ChunkMapping{
		CozeSliceID: 5001, RagChunkID: "rag-chunk-1", RagDocID: "rag-doc-1", CozeDocID: 200,
	}, 0))

	require.NoError(t, i.UpdateSlice(ctx, &service.UpdateSliceRequest{
		SliceID:    5001,
		DocumentID: 200,
		RawContent: []*knowledgeModel.SliceContent{{Type: knowledgeModel.SliceContentTypeText, Text: strPtr("new")}},
	}))
	require.NotNil(t, fc.updateChunkReq)
	require.NotNil(t, fc.updateChunkReq.Content)
	require.Equal(t, "new", *fc.updateChunkReq.Content)
	require.Equal(t, "rag-chunk-1", fc.updateChunkChunkID)
}

func TestUpdateSlice_MissingMapping(t *testing.T) {
	fc := &fakeClient{}
	i := newTestImpl(t, fc)
	ctx := context.Background()
	// No ChunkMapping inserted.
	err := i.UpdateSlice(ctx, &service.UpdateSliceRequest{
		SliceID:    9999,
		RawContent: []*knowledgeModel.SliceContent{{Type: knowledgeModel.SliceContentTypeText, Text: strPtr("x")}},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrMappingNotFound)
	require.Nil(t, fc.updateChunkReq)
}

// --- DeleteSlice ----------------------------------------------------------

func TestDeleteSlice_HappyPath(t *testing.T) {
	fc := &fakeClient{}
	i := newTestImpl(t, fc)
	ctx := context.Background()
	seedKBAndDoc(t, i, 100, "rag-kb-1", 200, "rag-doc-1")
	require.NoError(t, i.mapping.ChunkInsert(ctx, &ChunkMapping{
		CozeSliceID: 5001, RagChunkID: "rag-chunk-1", RagDocID: "rag-doc-1", CozeDocID: 200,
	}, 0))

	require.NoError(t, i.DeleteSlice(ctx, &service.DeleteSliceRequest{SliceID: 5001}))
	require.Equal(t, "rag-chunk-1", fc.deleteChunkChunkID)
	require.Equal(t, "rag-doc-1", fc.deleteChunkDocID)
	require.Equal(t, "rag-kb-1", fc.deleteChunkKBID)

	// Mapping is soft-deleted.
	_, err := i.mapping.ChunkByCozeID(ctx, 5001)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrMappingNotFound)
}

func TestDeleteSlice_RagFailure_NoSoftDelete(t *testing.T) {
	fc := &fakeClient{
		deleteChunkFunc: func(_, _, _, _ string) error { return errors.New("rag 50001") },
	}
	i := newTestImpl(t, fc)
	ctx := context.Background()
	seedKBAndDoc(t, i, 100, "rag-kb-1", 200, "rag-doc-1")
	require.NoError(t, i.mapping.ChunkInsert(ctx, &ChunkMapping{
		CozeSliceID: 5001, RagChunkID: "rag-chunk-1", RagDocID: "rag-doc-1", CozeDocID: 200,
	}, 0))

	require.Error(t, i.DeleteSlice(ctx, &service.DeleteSliceRequest{SliceID: 5001}))
	// Mapping must NOT be soft-deleted -- the rag delete failed, so the
	// chunk still exists rag-side. If we'd soft-deleted, the next read
	// would return "not found" while the chunk is still queryable.
	cm, err := i.mapping.ChunkByCozeID(ctx, 5001)
	require.NoError(t, err)
	require.Equal(t, "rag-chunk-1", cm.RagChunkID)
}

// --- GetSlice -------------------------------------------------------------

func TestGetSlice_HappyPath(t *testing.T) {
	fc := &fakeClient{
		getChunkFunc: func(_, _, _ string) (*contract.Chunk, error) {
			return &contract.Chunk{ChunkID: "rag-chunk-1", DocID: "rag-doc-1", KBID: "rag-kb-1",
				DocName: "design.pdf", ChunkType: "text_chunk", Content: "hello world",
				CharCount: 11, ByteCount: 11, Status: "ready"}, nil
		},
	}
	i := newTestImpl(t, fc)
	ctx := context.Background()
	seedKBAndDoc(t, i, 100, "rag-kb-1", 200, "rag-doc-1")
	require.NoError(t, i.mapping.ChunkInsert(ctx, &ChunkMapping{
		CozeSliceID: 5001, RagChunkID: "rag-chunk-1", RagDocID: "rag-doc-1", CozeDocID: 200,
	}, 0))

	got, err := i.GetSlice(ctx, &service.GetSliceRequest{SliceID: 5001})
	require.NoError(t, err)
	require.NotNil(t, got.Slice)
	require.Equal(t, int64(5001), got.Slice.Info.ID)
	require.Equal(t, int64(200), got.Slice.DocumentID)
	require.Equal(t, int64(100), got.Slice.KnowledgeID)
	require.Len(t, got.Slice.RawContent, 1)
	require.Equal(t, "hello world", *got.Slice.RawContent[0].Text)
}

func TestGetSlice_MissingMapping(t *testing.T) {
	fc := &fakeClient{}
	i := newTestImpl(t, fc)
	_, err := i.GetSlice(context.Background(), &service.GetSliceRequest{SliceID: 9999})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrMappingNotFound)
}

// --- MGetSlice ------------------------------------------------------------

func TestMGetSlice_GroupsByKB(t *testing.T) {
	// Two KBs each with one doc; one slice per doc. MGetSlice should issue
	// a separate rag call per KB.
	var calls []string
	fc := &fakeClient{
		mgetChunksFunc: func(_, kbID string, chunkIDs []string) (*contract.MGetChunksResponse, error) {
			calls = append(calls, kbID)
			items := make([]contract.MGetChunksItem, 0, len(chunkIDs))
			for _, id := range chunkIDs {
				items = append(items, contract.MGetChunksItem{Chunk: contract.Chunk{ChunkID: id, KBID: kbID, ChunkType: "text_chunk", Content: "hi"}})
			}
			return &contract.MGetChunksResponse{Items: items}, nil
		},
	}
	i := newTestImpl(t, fc)
	ctx := context.Background()
	seedKBAndDoc(t, i, 100, "rag-kb-A", 200, "rag-doc-A")
	seedKBAndDoc(t, i, 101, "rag-kb-B", 201, "rag-doc-B")
	require.NoError(t, i.mapping.ChunkInsert(ctx, &ChunkMapping{CozeSliceID: 5001, RagChunkID: "c-A", RagDocID: "rag-doc-A", CozeDocID: 200}, 0))
	require.NoError(t, i.mapping.ChunkInsert(ctx, &ChunkMapping{CozeSliceID: 5002, RagChunkID: "c-B", RagDocID: "rag-doc-B", CozeDocID: 201}, 0))

	got, err := i.MGetSlice(ctx, &service.MGetSliceRequest{SliceIDs: []int64{5001, 5002}})
	require.NoError(t, err)
	require.Len(t, got.Slices, 2)
	require.ElementsMatch(t, []string{"rag-kb-A", "rag-kb-B"}, calls)
}

func TestMGetSlice_SkipsDeletedItems(t *testing.T) {
	fc := &fakeClient{
		mgetChunksFunc: func(_, kbID string, chunkIDs []string) (*contract.MGetChunksResponse, error) {
			items := []contract.MGetChunksItem{
				{Chunk: contract.Chunk{ChunkID: chunkIDs[0], KBID: kbID, ChunkType: "text_chunk", Content: "a"}},
				{Chunk: contract.Chunk{ChunkID: chunkIDs[1]}, Deleted: true},
			}
			return &contract.MGetChunksResponse{Items: items}, nil
		},
	}
	i := newTestImpl(t, fc)
	ctx := context.Background()
	seedKBAndDoc(t, i, 100, "rag-kb-1", 200, "rag-doc-1")
	require.NoError(t, i.mapping.ChunkInsert(ctx, &ChunkMapping{CozeSliceID: 1, RagChunkID: "c-1", RagDocID: "rag-doc-1", CozeDocID: 200}, 0))
	require.NoError(t, i.mapping.ChunkInsert(ctx, &ChunkMapping{CozeSliceID: 2, RagChunkID: "c-2", RagDocID: "rag-doc-1", CozeDocID: 200}, 0))

	got, err := i.MGetSlice(ctx, &service.MGetSliceRequest{SliceIDs: []int64{1, 2}})
	require.NoError(t, err)
	require.Len(t, got.Slices, 1, "deleted placeholders must be skipped")
	require.Equal(t, int64(1), got.Slices[0].Info.ID)
}

func TestMGetSlice_EmptyInput(t *testing.T) {
	fc := &fakeClient{}
	i := newTestImpl(t, fc)
	got, err := i.MGetSlice(context.Background(), &service.MGetSliceRequest{SliceIDs: nil})
	require.NoError(t, err)
	require.Empty(t, got.Slices)
	require.Empty(t, fc.mgetChunksIDs, "rag must not be called for empty input")
}

// --- ListSlice ------------------------------------------------------------

func TestListSlice_HappyPath_LazyBackfill(t *testing.T) {
	// One pre-existing mapping (chunk-a), one unmapped (chunk-b). The
	// unmapped one gets a fresh id allocated; the mapped one keeps its id.
	fc := &fakeClient{
		listChunksFunc: func(_, _, _ string, _ *contract.ListChunksQuery) (*contract.ListChunksResponse, error) {
			return &contract.ListChunksResponse{
				Items: []contract.Chunk{
					{ChunkID: "chunk-a", DocID: "rag-doc-1", ChunkType: "text_chunk", Content: "A"},
					{ChunkID: "chunk-b", DocID: "rag-doc-1", ChunkType: "text_chunk", Content: "B"},
				},
				Total: 2,
			}, nil
		},
	}
	i := newTestImpl(t, fc, 7777) // single id available for the lazy backfill
	ctx := context.Background()
	seedKBAndDoc(t, i, 100, "rag-kb-1", 200, "rag-doc-1")
	require.NoError(t, i.mapping.ChunkInsert(ctx, &ChunkMapping{CozeSliceID: 5001, RagChunkID: "chunk-a", RagDocID: "rag-doc-1", CozeDocID: 200}, 0))

	docID := int64(200)
	got, err := i.ListSlice(ctx, &service.ListSliceRequest{DocumentID: &docID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, got.Slices, 2)
	require.Equal(t, int64(5001), got.Slices[0].Info.ID, "pre-existing mapping must be reused")
	require.Equal(t, int64(7777), got.Slices[1].Info.ID, "lazy backfill must allocate via idgen")

	// Verify the new mapping row landed.
	cm, err := i.mapping.ChunkByRagID(ctx, "chunk-b")
	require.NoError(t, err)
	require.Equal(t, int64(7777), cm.CozeSliceID)
}

func TestListSlice_RequiresDocumentID(t *testing.T) {
	fc := &fakeClient{}
	i := newTestImpl(t, fc)
	_, err := i.ListSlice(context.Background(), &service.ListSliceRequest{})
	require.Error(t, err)
}

// --- ListPhotoSlice -------------------------------------------------------
//
// The new implementation paginates rag documents (not image_chunks). One
// synthetic slice is returned per document. The slice's RawContent[0].Text
// is the document filename; its Info.ID is the coze_doc_id from the mapping
// (not a freshly-allocated chunk id).

func TestRagimplListPhotoSlice_ReturnsOneSlicePerDocument(t *testing.T) {
	// Three docs in rag; all three have mapping rows.
	fc := &fakeClient{
		listDocsFunc: func(_, _ string, page, pageSize int) (*contract.ListDocumentsResponse, error) {
			return &contract.ListDocumentsResponse{
				Items: []contract.Document{
					{DocID: "rd1", Filename: "dog.png", Status: "ready"},
					{DocID: "rd2", Filename: "cat.jpg", Status: "ready"},
					{DocID: "rd3", Filename: "bird.webp", Status: "ready"},
				},
				Total: 3,
			}, nil
		},
	}
	i := newTestImpl(t, fc)
	ctx := context.Background()
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-1", "", 0, 0, 0, knowledgeModel.DocumentTypeImage))
	require.NoError(t, i.mapping.InsertDoc(ctx, 1001, "rd1", 100, 7, "", 0, 0, ""))
	require.NoError(t, i.mapping.InsertDoc(ctx, 1002, "rd2", 100, 7, "", 0, 0, ""))
	require.NoError(t, i.mapping.InsertDoc(ctx, 1003, "rd3", 100, 7, "", 0, 0, ""))

	limit := 20
	offset := 0
	got, err := i.ListPhotoSlice(ctx, &service.ListPhotoSliceRequest{
		KnowledgeID: 100,
		Limit:       &limit,
		Offset:      &offset,
	})
	require.NoError(t, err)
	require.Equal(t, 3, got.Total)
	require.Len(t, got.Slices, 3, "one slice per document")

	// Build a map from coze_doc_id to slice for assertions.
	byDocID := map[int64]*entity.Slice{}
	for _, s := range got.Slices {
		byDocID[s.DocumentID] = s
	}
	require.Contains(t, byDocID, int64(1001))
	require.Contains(t, byDocID, int64(1002))
	require.Contains(t, byDocID, int64(1003))

	// Each slice's RawContent[0].Text should be the filename.
	require.NotEmpty(t, byDocID[1001].RawContent)
	require.NotNil(t, byDocID[1001].RawContent[0].Text)
	require.Equal(t, "dog.png", *byDocID[1001].RawContent[0].Text)
}

func TestRagimplListPhotoSlice_HonorsLimitOffset(t *testing.T) {
	// 5 docs total; request page 2 (offset=2, limit=2) -> 2 slices.
	allDocs := []contract.Document{
		{DocID: "rd1", Filename: "a.png", Status: "ready"},
		{DocID: "rd2", Filename: "b.png", Status: "ready"},
		{DocID: "rd3", Filename: "c.png", Status: "ready"},
		{DocID: "rd4", Filename: "d.png", Status: "ready"},
		{DocID: "rd5", Filename: "e.png", Status: "ready"},
	}
	fc := &fakeClient{
		listDocsFunc: func(_, _ string, page, pageSize int) (*contract.ListDocumentsResponse, error) {
			start := (page - 1) * pageSize
			end := start + pageSize
			if start >= len(allDocs) {
				return &contract.ListDocumentsResponse{Total: len(allDocs)}, nil
			}
			if end > len(allDocs) {
				end = len(allDocs)
			}
			return &contract.ListDocumentsResponse{
				Items: allDocs[start:end],
				Total: len(allDocs),
			}, nil
		},
	}
	i := newTestImpl(t, fc)
	ctx := context.Background()
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-1", "", 0, 0, 0, knowledgeModel.DocumentTypeImage))
	for idx, id := range []int64{2001, 2002, 2003, 2004, 2005} {
		require.NoError(t, i.mapping.InsertDoc(ctx, id, allDocs[idx].DocID, 100, 7, "", 0, 0, ""))
	}

	limit := 2
	offset := 2
	got, err := i.ListPhotoSlice(ctx, &service.ListPhotoSliceRequest{
		KnowledgeID: 100,
		Limit:       &limit,
		Offset:      &offset,
	})
	require.NoError(t, err)
	require.Equal(t, 5, got.Total)
	require.Len(t, got.Slices, 2, "limit=2 means 2 slices")
}

func TestRagimplListPhotoSlice_SkipsOrphanRagDocs(t *testing.T) {
	// rag returns 3 docs, but only 2 have mapping rows; the 3rd is an orphan.
	fc := &fakeClient{
		listDocsFunc: func(_, _ string, page, pageSize int) (*contract.ListDocumentsResponse, error) {
			return &contract.ListDocumentsResponse{
				Items: []contract.Document{
					{DocID: "rd1", Filename: "kept-1.png", Status: "ready"},
					{DocID: "rd-orphan", Filename: "orphan.png", Status: "ready"},
					{DocID: "rd2", Filename: "kept-2.png", Status: "ready"},
				},
				Total: 3,
			}, nil
		},
	}
	i := newTestImpl(t, fc)
	ctx := context.Background()
	require.NoError(t, i.mapping.InsertKB(ctx, 100, "rag-kb-1", "", 0, 0, 0, knowledgeModel.DocumentTypeImage))
	require.NoError(t, i.mapping.InsertDoc(ctx, 3001, "rd1", 100, 7, "", 0, 0, ""))
	require.NoError(t, i.mapping.InsertDoc(ctx, 3002, "rd2", 100, 7, "", 0, 0, ""))
	// rd-orphan has NO mapping row.

	limit := 20
	got, err := i.ListPhotoSlice(ctx, &service.ListPhotoSliceRequest{
		KnowledgeID: 100,
		Limit:       &limit,
	})
	require.NoError(t, err)
	require.Len(t, got.Slices, 2, "orphan must be skipped")
	require.Equal(t, 3, got.Total, "total still reflects rag's document count")
}

// --- Concurrency: lazy backfill convergence -------------------------------

// TestListSlice_LazyBackfill_RaceConvergence simulates the race between two
// ListSlice calls: the first call assigns id=N to chunk-x; before the second
// call's existing-row check fires we hand-insert a duplicate row with id=N+1.
// The second call's re-resolve must converge on N.
func TestListSlice_LazyBackfill_RaceConvergence(t *testing.T) {
	fc := &fakeClient{
		listChunksFunc: func(_, _, _ string, _ *contract.ListChunksQuery) (*contract.ListChunksResponse, error) {
			return &contract.ListChunksResponse{
				Items: []contract.Chunk{{ChunkID: "chunk-x", DocID: "rag-doc-1", ChunkType: "text_chunk", Content: "X"}},
				Total: 1,
			}, nil
		},
	}
	i := newTestImpl(t, fc, 5000, 5001)
	ctx := context.Background()
	seedKBAndDoc(t, i, 100, "rag-kb-1", 200, "rag-doc-1")

	docID := int64(200)
	got, err := i.ListSlice(ctx, &service.ListSliceRequest{DocumentID: &docID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, got.Slices, 1)
	require.Equal(t, int64(5000), got.Slices[0].Info.ID)

	// Simulate a racing insert that wrote a STRICTLY-LATER row for the same
	// chunk. ChunkByRagID orders by created_at ASC; we have to pick a ts
	// after the real insert (which used time.Now().UnixMilli()) so that the
	// racer is genuinely "later" and gets ignored.
	require.NoError(t, i.mapping.ChunkInsert(ctx, &ChunkMapping{
		CozeSliceID: 5001, RagChunkID: "chunk-x", RagDocID: "rag-doc-1", CozeDocID: 200,
	}, 1<<62))

	// Subsequent ListSlice still resolves to the earliest (5000).
	got, err = i.ListSlice(ctx, &service.ListSliceRequest{DocumentID: &docID, Limit: 10})
	require.NoError(t, err)
	require.Equal(t, int64(5000), got.Slices[0].Info.ID, "all callers must converge on earliest-created mapping")
}
