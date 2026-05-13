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

package errno

import "github.com/coze-dev/coze-studio/backend/pkg/errorx/code"

// RAG integration: 105 100 000 ~ 105 199 999
const (
	ErrRagFeaturePendingCode      = 105100001
	ErrRagUpstreamUnavailableCode = 105100002
	ErrRagCrossTenantCode         = 105100003
	ErrRagMappingNotFoundCode     = 105100004
	ErrRagInvalidConfigCode       = 105100005
)

func init() {
	code.Register(
		ErrRagFeaturePendingCode,
		"feature pending rag support: {msg}",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrRagUpstreamUnavailableCode,
		"rag service unavailable: {msg}",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrRagCrossTenantCode,
		"cross-tenant retrieval rejected: {msg}",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrRagMappingNotFoundCode,
		"rag mapping not found: {msg}",
		code.WithAffectStability(false),
	)

	code.Register(
		ErrRagInvalidConfigCode,
		"invalid rag config: {msg}",
		code.WithAffectStability(false),
	)
}
