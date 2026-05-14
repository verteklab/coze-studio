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

package ragimpl

import (
	"context"

	contract "github.com/coze-dev/coze-studio/backend/infra/contract/rag"
)

// ListDocumentParameterSchemas returns rag's system-wide catalog of per-
// schema_id parameter forms. The rag endpoint is NOT KB-scoped, so this
// pass-through performs only a tenant resolver call before forwarding to
// the rag client.
//
// The response shape is the rag-side typed value; coze does not translate
// to an entity. R2-D-frontend will introduce the UI-side translation
// (or hide the indirection behind a service-layer DTO) when the wizard
// rework needs concrete data.
func (i *Impl) ListDocumentParameterSchemas(ctx context.Context) ([]contract.DocumentParameterSchema, error) {
	tenant, err := i.tenant(ctx)
	if err != nil {
		return nil, err
	}
	return i.rag.ListDocumentParameterSchemas(ctx, tenant)
}
