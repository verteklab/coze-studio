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

package rag

import (
	"fmt"

	"github.com/coze-dev/coze-studio/backend/pkg/errorx"
	"github.com/coze-dev/coze-studio/backend/types/errno"
)

// MapRagError converts a rag-side error response to a coze errno.
//
// rag codes follow the layout in 总体设计文档.md §12.6 (registry at
// rag/app/core/exceptions.py):
//
//	40001-40009  -> invalid param / capability mismatch
//	404xx        -> not found (40401=KB, 40402=document, 40403=task,
//	                40404=model_provider, 40405=chunk)
//	40901        -> KB name duplicate
//	40902        -> document status conflict (e.g. listing chunks on a
//	                pending document)
//	40904        -> content-type vs KB-capability mismatch (semantically a
//	                param error, not a duplicate)
//	other 409xx  -> conflict; default to "duplicate" bucket
//	default      -> upstream unavailable (covers 5xx and unrecognised codes)
//
// The original rag code is preserved in the {msg} extra so it remains visible
// in logs and surfaced error strings; we only collapse the dimension when
// mapping to the coze errno space.
func MapRagError(httpStatus, ragCode int, ragMessage string) error {
	switch {
	case ragCode >= 40001 && ragCode <= 40009:
		return errorx.New(errno.ErrKnowledgeInvalidParamCode,
			errorx.KV("msg", fmt.Sprintf("rag %d: %s", ragCode, ragMessage)))
	case ragCode >= 40400 && ragCode < 40500:
		switch ragCode {
		case 40401:
			return errorx.New(errno.ErrKnowledgeNotExistCode, errorx.KV("msg", ragMessage))
		case 40402:
			return errorx.New(errno.ErrKnowledgeDocumentNotExistCode, errorx.KV("msg", ragMessage))
		default:
			return errorx.New(errno.ErrKnowledgeNotExistCode,
				errorx.KV("msg", fmt.Sprintf("rag %d: %s", ragCode, ragMessage)))
		}
	case ragCode >= 40900 && ragCode < 41000:
		switch ragCode {
		case 40902:
			// "document is not in a state that allows this operation" --
			// e.g. ListSlice on a pending doc, manual chunk edit before
			// ingestion finishes. ErrKnowledgeDocNotReadyCode carries the
			// right semantic; the previous mapping to ErrKnowledgeDuplicateCode
			// surfaced as "knowledge name duplicate" which was misleading.
			return errorx.New(errno.ErrKnowledgeDocNotReadyCode,
				errorx.KV("msg", fmt.Sprintf("rag %d: %s", ragCode, ragMessage)))
		case 40904:
			// "content type does not match the KB capabilities" -- this is a
			// param/capability rejection, not a duplicate. Falls under the
			// same coze bucket as 40005 (model binding mismatch) etc.
			return errorx.New(errno.ErrKnowledgeInvalidParamCode,
				errorx.KV("msg", fmt.Sprintf("rag %d: %s", ragCode, ragMessage)))
		default:
			return errorx.New(errno.ErrKnowledgeDuplicateCode,
				errorx.KV("msg", fmt.Sprintf("rag %d: %s", ragCode, ragMessage)))
		}
	default:
		return errorx.New(errno.ErrRagUpstreamUnavailableCode,
			errorx.KV("msg", fmt.Sprintf("http=%d rag=%d msg=%s", httpStatus, ragCode, ragMessage)))
	}
}
