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

	"gorm.io/gorm"

	"github.com/coze-dev/coze-studio/backend/domain/knowledge/entity"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/service"
	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
	"github.com/coze-dev/coze-studio/backend/infra/idgen"
	"github.com/coze-dev/coze-studio/backend/infra/storage"
)

// Compile-time check: Impl satisfies the wider Knowledge interface. If a new
// method is added to service.Knowledge, this line breaks the build until
// either a real implementation or a bucket-B stub (unsupported.go) lands.
var _ service.Knowledge = (*Impl)(nil)

type Impl struct {
	rag      contract.Client
	mapping  *MappingRepo
	idgen    idgen.IDGenerator
	resolver TenantResolver
	// storage is used by CreateDocument to fetch file bytes from MinIO and
	// forward them to rag as a multipart body. Required since the 2026-05-14
	// rag contract change; previously rag fetched by source_uri itself.
	storage storage.Storage

	defaultTextEmbeddingModelID  string
	defaultImageEmbeddingModelID string
}

func New(
	rag contract.Client,
	db *gorm.DB,
	idgen idgen.IDGenerator,
	resolver TenantResolver,
	storage storage.Storage,
	defaultTextModel, defaultImageModel string,
) *Impl {
	return &Impl{
		rag:                          rag,
		mapping:                      NewMappingRepo(db),
		idgen:                        idgen,
		resolver:                     resolver,
		storage:                      storage,
		defaultTextEmbeddingModelID:  defaultTextModel,
		defaultImageEmbeddingModelID: defaultImageModel,
	}
}

// tenant returns the rag tenant_id for the current request, via the resolver.
// All rag calls in KB / Document / Retrieval methods MUST go through this — never
// derive a tenant_id from request fields (e.g. SpaceID) or from mapping rows.
func (i *Impl) tenant(ctx context.Context) (string, error) {
	return i.resolver.Resolve(ctx)
}

// RagStatusToEntity maps rag's document status string to coze's enum.
//
// Mapping table (rag → coze):
//
//	pending    → DocumentStatusInit      (queued, not yet picked up)
//	processing → DocumentStatusChunking  (rag is parsing/chunking/embedding)
//	ready      → DocumentStatusEnable    (indexed and queryable)
//	failed     → DocumentStatusFailed
//	*unknown*  → DocumentStatusFailed    (fail closed; surfaces drift in rag's API)
//
// Note: coze has no dedicated "Processing" status, so rag's "processing" reuses
// DocumentStatusChunking (value 4, "切片中" / "Slicing"). That label is the closest
// fit since chunking dominates the rag pipeline's processing phase; if rag later
// distinguishes parse vs. embed phases this mapping should be revisited.
func RagStatusToEntity(s string) entity.DocumentStatus {
	switch s {
	case "pending":
		return entity.DocumentStatusInit
	case "processing":
		return entity.DocumentStatusChunking
	case "ready":
		return entity.DocumentStatusEnable
	case "failed":
		return entity.DocumentStatusFailed
	default:
		return entity.DocumentStatusFailed
	}
}

// progressForStatus maps rag's task status string to a coarse 0-100 progress
// value for UI display. Rag dropped its numeric progress field in 0e1f49b; this
// is the best approximation until /capabilities exposes per-phase progress
// (planned for R2-D).
//
// Pending shows a small non-zero so the UI's progress bar isn't visually
// indistinguishable from "no doc yet." Failed maps to 0 because a failed bar
// at 100% would be misleading; the failure state is communicated separately
// via dp.Status + dp.StatusMsg.
func progressForStatus(s string) int {
	switch s {
	case "pending":
		return 10
	case "running", "retrying":
		return 50
	case "success":
		return 100
	case "failed":
		return 0
	default:
		return 0
	}
}
