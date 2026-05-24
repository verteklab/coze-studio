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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/coze-dev/coze-studio/backend/pkg/errorx"
	"github.com/coze-dev/coze-studio/backend/types/errno"
)

func TestCheckReadAccess_LoggedIn(t *testing.T) {
	svc := &KnowledgeApplicationService{}
	uid := int64(42)
	err := svc.checkReadAccess(context.Background(), &uid)
	assert.NoError(t, err)
}

func TestCheckReadAccess_Unauthenticated(t *testing.T) {
	svc := &KnowledgeApplicationService{}
	err := svc.checkReadAccess(context.Background(), nil)
	assert.Error(t, err)

	// errorx.FromStatusError doesn't exist in this codebase; use the
	// errors.As pattern that backend/pkg/errorx/error_test.go uses to
	// reach the StatusError interface.
	var statusErr errorx.StatusError
	ok := errors.As(err, &statusErr)
	assert.True(t, ok)
	assert.Equal(t, int32(errno.ErrKnowledgePermissionCode), statusErr.Code())
}

func TestCheckWriteAccess_Unauthenticated(t *testing.T) {
	svc := &KnowledgeApplicationService{}
	err := svc.checkWriteAccess(context.Background(), nil, nil, nil, nil)
	assert.Error(t, err)
}

func TestCheckWriteAccess_NoTarget(t *testing.T) {
	svc := &KnowledgeApplicationService{}
	uid := int64(42)
	err := svc.checkWriteAccess(context.Background(), &uid, nil, nil, nil)
	assert.Error(t, err)

	var statusErr errorx.StatusError
	if assert.True(t, errors.As(err, &statusErr)) {
		assert.Equal(t, int32(errno.ErrKnowledgePermissionCode), statusErr.Code())
	}
}
