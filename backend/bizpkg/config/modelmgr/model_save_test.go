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

package modelmgr

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/coze-dev/coze-studio/backend/api/model/app/developer_api"
)

func TestPickCapability_RequestPriority(t *testing.T) {
	truthy := true
	reqCap := &developer_api.ModelAbility{ImageUnderstanding: &truthy}
	metaCap := &developer_api.ModelAbility{FunctionCall: &truthy}

	got := pickCapability(reqCap, metaCap)
	assert.Equal(t, reqCap, got, "request capability must override meta default")
}

func TestPickCapability_FallbackToMeta(t *testing.T) {
	truthy := true
	metaCap := &developer_api.ModelAbility{FunctionCall: &truthy}

	got := pickCapability(nil, metaCap)
	assert.Equal(t, metaCap, got, "nil request must fall back to meta")
}

func TestPickCapability_BothNil(t *testing.T) {
	got := pickCapability(nil, nil)
	assert.Nil(t, got)
}
