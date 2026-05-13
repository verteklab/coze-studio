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

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/coze-dev/coze-studio/backend/domain/workflow/entity/vo"
	"github.com/coze-dev/coze-studio/backend/pkg/errorx"
	"github.com/coze-dev/coze-studio/backend/pkg/lang/ptr"
	"github.com/coze-dev/coze-studio/backend/types/errno"

	mock_workflow "github.com/coze-dev/coze-studio/backend/internal/mock/domain/workflow"
)

// TestImpl_Create_DuplicateName_ReturnsErrno covers AC2a:
// Same creator already owns a workflow with the same name (not soft-deleted),
// a second Create must return errno.ErrWorkflowNameIsDuplicated.
func TestImpl_Create_DuplicateName_ReturnsErrno(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockRepo := mock_workflow.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		IsWorkflowNameDuplicated(gomock.Any(), int64(1001), "translate-workflow", (*int64)(nil)).
		Return(true, nil).
		Times(1)

	testImpl := &impl{repo: mockRepo}

	_, err := testImpl.Create(ctx, &vo.MetaCreate{
		Name:      "translate-workflow",
		CreatorID: 1001,
		SpaceID:   1001,
	})

	assert.Error(t, err)
	var statusErr errorx.StatusError
	ok := errors.As(err, &statusErr)
	assert.True(t, ok, "expected error to satisfy errorx.StatusError")
	assert.Equal(t, int32(errno.ErrWorkflowNameIsDuplicated), statusErr.Code())
}

// TestImpl_UpdateMeta_RenameToSelf_IsNoOp covers AC2c:
// Renaming a workflow to its own current name must not error and must skip
// the duplicate check entirely (excludeID hits self).
func TestImpl_UpdateMeta_RenameToSelf_IsNoOp(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	const wfID int64 = 9001
	const sameName = "my-flow"

	mockRepo := mock_workflow.NewMockRepository(ctrl)
	mockRepo.EXPECT().
		GetMeta(gomock.Any(), wfID).
		Return(&vo.Meta{
			Name:      sameName,
			CreatorID: 2002,
		}, nil).
		Times(1)
	// Critical: duplicate check must NOT run when the name is unchanged.
	mockRepo.EXPECT().
		IsWorkflowNameDuplicated(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(0)
	mockRepo.EXPECT().
		UpdateMeta(gomock.Any(), wfID, gomock.Any()).
		Return(nil).
		Times(1)

	testImpl := &impl{repo: mockRepo}

	err := testImpl.UpdateMeta(ctx, wfID, &vo.MetaUpdate{
		Name: ptr.Of(sameName),
	})

	assert.NoError(t, err)
}

// TestImpl_Create_DuplicateName_AcrossUsers_NotBlocked covers AC2b:
// Two different creators may use the same workflow name; the duplicate-check
// is scoped per-creator, so creator B's Create must NOT trip the duplicate
// errno even when creator A already owns the same name.
//
// We stub IsWorkflowNameDuplicated to return false (the repo layer is
// responsible for the per-creator SQL scoping; here we just assert the
// service propagates that "available" verdict). To avoid exercising Save's
// canvas-parsing path, we short-circuit by having CreateMeta return a
// sentinel error and assert that the returned error is the sentinel
// (i.e. NOT the duplicate-name errno).
func TestImpl_Create_DuplicateName_AcrossUsers_NotBlocked(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	sentinel := errors.New("createmeta short-circuit")

	mockRepo := mock_workflow.NewMockRepository(ctrl)
	// Creator B asking about a name creator A also uses: repo (scoped by
	// creator_id) reports "not duplicated for creator B".
	mockRepo.EXPECT().
		IsWorkflowNameDuplicated(gomock.Any(), int64(7002), "foo", (*int64)(nil)).
		Return(false, nil).
		Times(1)
	mockRepo.EXPECT().
		CreateMeta(gomock.Any(), gomock.Any()).
		Return(int64(0), sentinel).
		Times(1)

	testImpl := &impl{repo: mockRepo}

	_, err := testImpl.Create(ctx, &vo.MetaCreate{
		Name:      "foo",
		CreatorID: 7002, // creator B; creator A (some other id) also owns "foo"
		SpaceID:   7002,
	})

	// Must surface the sentinel from CreateMeta — meaning the duplicate-name
	// check did NOT block creator B.
	assert.ErrorIs(t, err, sentinel)

	// And, defensively, it must NOT be the duplicate-name errno.
	var statusErr errorx.StatusError
	if errors.As(err, &statusErr) {
		assert.NotEqual(t, int32(errno.ErrWorkflowNameIsDuplicated), statusErr.Code(),
			"cross-user create must not return ErrWorkflowNameIsDuplicated")
	}
}
