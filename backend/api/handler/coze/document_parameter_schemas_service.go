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

package coze

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	application "github.com/coze-dev/coze-studio/backend/application/knowledge"
)

// ListRagDocumentParameterSchemas proxies rag's GET /document-parameter-schemas
// endpoint so the upload wizard can render a dynamic "advanced parsing"
// panel per file type.
//
// Route: GET /api/knowledge/rag/document_parameter_schemas
//
// When KNOWLEDGE_BACKEND=legacy, the application service returns an
// ErrRagFeaturePendingCode and the caller falls back to the static
// parsing-strategy UI.
func ListRagDocumentParameterSchemas(ctx context.Context, c *app.RequestContext) {
	resp, err := application.KnowledgeSVC.ListRagDocumentParameterSchemas(ctx)
	if err != nil {
		internalServerErrorResponse(ctx, c, err)
		return
	}
	c.JSON(consts.StatusOK, resp)
}
