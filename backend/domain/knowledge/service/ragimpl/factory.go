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
	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
	"github.com/coze-dev/coze-studio/backend/infra/idgen"
)

// Compile-time check: Impl satisfies the wider Knowledge interface.
// Commented for Task 10B — Tasks 11-14 implement the required methods;
// Task 14 restores this assertion.
// var _ service.Knowledge = (*Impl)(nil)

type Impl struct {
	rag      contract.Client
	mapping  *MappingRepo
	idgen    idgen.IDGenerator
	resolver TenantResolver

	defaultTextEmbeddingModelID  string
	defaultImageEmbeddingModelID string
}

func New(
	rag contract.Client,
	db *gorm.DB,
	idgen idgen.IDGenerator,
	resolver TenantResolver,
	defaultTextModel, defaultImageModel string,
) *Impl {
	return &Impl{
		rag:                          rag,
		mapping:                      NewMappingRepo(db),
		idgen:                        idgen,
		resolver:                     resolver,
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
