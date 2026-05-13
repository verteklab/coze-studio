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

//go:build integration

// Package ragimpl integration test.
//
// This file is gated by the `integration` build tag *and* the INTEGRATION=1
// environment variable, so it is skipped by `go test ./...` and only runs
// when explicitly invoked against a real rag service + MySQL.
//
// Required environment:
//
//	INTEGRATION=1
//	MYSQL_DSN=user:pass@tcp(host:port)/db?parseTime=true
//	RAG_BASE_URL=http://localhost:8000
//	RAG_DEFAULT_TEXT_EMBEDDING_MODEL_ID=...
//	RAG_DEFAULT_IMAGE_EMBEDDING_MODEL_ID=...
//	SMOKE_DOC_URI=<minio key referencing a tiny text file>
//
// Run with:
//
//	GOTOOLCHAIN=go1.24.0 go test -tags=integration \
//	    ./domain/knowledge/service/ragimpl/... -run TestIntegration_EndToEnd -v

package ragimpl

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	ragconf "github.com/coze-dev/coze-studio/backend/conf/rag"
	knowledgeModel "github.com/coze-dev/coze-studio/backend/crossdomain/knowledge/model"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/entity"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/service"
	infrarag "github.com/coze-dev/coze-studio/backend/infra/rag"
)

// integrationIDGen is a wall-clock-based ID generator for integration runs.
// It avoids pulling in the production idgen wiring (which requires snowflake
// init / config) while still producing monotonically-increasing int64 IDs.
type integrationIDGen struct{}

func (integrationIDGen) GenID(_ context.Context) (int64, error) {
	return time.Now().UnixNano(), nil
}

func (integrationIDGen) GenMultiIDs(_ context.Context, n int) ([]int64, error) {
	out := make([]int64, n)
	base := time.Now().UnixNano()
	for i := 0; i < n; i++ {
		out[i] = base + int64(i)
	}
	return out, nil
}

func TestIntegration_EndToEnd(t *testing.T) {
	if os.Getenv("INTEGRATION") != "1" {
		t.Skip("set INTEGRATION=1 to run")
	}

	dsn := os.Getenv("MYSQL_DSN")
	require.NotEmpty(t, dsn, "MYSQL_DSN must be set")
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	timeoutMs := 30000
	cfg := ragconf.Config{
		BaseURL:            os.Getenv("RAG_BASE_URL"),
		Timeout:            time.Duration(timeoutMs) * time.Millisecond,
		TimeoutMs:          timeoutMs,
		UploadTimeoutMs:    60000,
		RetrievalTimeoutMs: 15000,
	}
	require.NotEmpty(t, cfg.BaseURL, "RAG_BASE_URL must be set")
	client := infrarag.New(cfg)

	ctx := context.Background()
	require.NoError(t, client.Ready(ctx), "rag service /ready must succeed")

	resolver := NewEnvTenantResolver("coze-it")
	impl := New(
		client,
		db,
		integrationIDGen{},
		resolver,
		os.Getenv("RAG_DEFAULT_TEXT_EMBEDDING_MODEL_ID"),
		os.Getenv("RAG_DEFAULT_IMAGE_EMBEDDING_MODEL_ID"),
	)

	// Create KB.
	cr, err := impl.CreateKnowledge(ctx, &service.CreateKnowledgeRequest{
		Name:       "it-kb",
		SpaceID:    99,
		CreatorID:  1,
		FormatType: knowledgeModel.DocumentTypeText,
	})
	require.NoError(t, err)
	require.NotZero(t, cr.KnowledgeID)

	// Cleanup at the end no matter what happens after this point.
	t.Cleanup(func() {
		_ = impl.DeleteKnowledge(context.Background(), &service.DeleteKnowledgeRequest{
			KnowledgeID: cr.KnowledgeID,
		})
	})

	// Upload one tiny document. SMOKE_DOC_URI is expected to already exist in
	// the minio bucket that rag is configured to read from.
	docURI := os.Getenv("SMOKE_DOC_URI")
	require.NotEmpty(t, docURI, "SMOKE_DOC_URI must be set")

	doc := &entity.Document{
		KnowledgeID: cr.KnowledgeID,
		URI:         docURI,
	}
	doc.Name = "smoke.txt"
	doc.CreatorID = 1

	_, err = impl.CreateDocument(ctx, &service.CreateDocumentRequest{
		Documents: []*entity.Document{doc},
	})
	require.NoError(t, err)
	require.NotZero(t, doc.ID, "CreateDocument must populate the document ID")

	// Poll for ingestion completion. The deadline is generous (2 min) to
	// accommodate cold-start embedding models.
	deadline := time.Now().Add(2 * time.Minute)
	var lastStatus entity.DocumentStatus
	for time.Now().Before(deadline) {
		pr, perr := impl.MGetDocumentProgress(ctx, &service.MGetDocumentProgressRequest{
			DocumentIDs: []int64{doc.ID},
		})
		require.NoError(t, perr)
		require.NotEmpty(t, pr.ProgressList)
		lastStatus = pr.ProgressList[0].Status
		if lastStatus == entity.DocumentStatusEnable {
			break
		}
		require.NotEqual(t, entity.DocumentStatusFailed, lastStatus, "ingestion failed: %s", pr.ProgressList[0].StatusMsg)
		time.Sleep(2 * time.Second)
	}
	require.Equal(t, entity.DocumentStatusEnable, lastStatus, "document did not reach Enable status within deadline")

	// Retrieve.
	rr, err := impl.Retrieve(ctx, &knowledgeModel.RetrieveRequest{
		Query:        "smoke",
		KnowledgeIDs: []int64{cr.KnowledgeID},
	})
	require.NoError(t, err)
	require.NotEmpty(t, rr.RetrieveSlices, "retrieval returned zero slices")
}
