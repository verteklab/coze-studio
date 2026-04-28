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

package impl

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coze-dev/coze-studio/backend/api/model/admin/config"
	"github.com/coze-dev/coze-studio/backend/pkg/openaiproxy"
)

func TestGetEmbeddingReturnsProxyError(t *testing.T) {
	t.Setenv(openaiproxy.EnvKey, "://bad proxy")

	embedder, err := GetEmbedding(context.Background(), &config.EmbeddingConfig{
		Type:         config.EmbeddingType_OpenAI,
		MaxBatchSize: 10,
		Connection: &config.EmbeddingConnection{
			BaseConnInfo: &config.BaseConnectionInfo{
				APIKey: "secret",
				Model:  "text-embedding-3-small",
			},
			EmbeddingInfo: &config.EmbeddingInfo{Dims: 1536},
			Openai:        &config.OpenAIConnInfo{},
		},
	})
	require.ErrorContains(t, err, openaiproxy.EnvKey)
	require.Nil(t, embedder)
}
