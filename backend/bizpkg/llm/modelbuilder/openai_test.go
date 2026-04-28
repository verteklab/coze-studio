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

package modelbuilder

import (
	"context"
	"testing"

	modelopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/stretchr/testify/require"

	"github.com/coze-dev/coze-studio/backend/api/model/admin/config"
	"github.com/coze-dev/coze-studio/backend/pkg/lang/ptr"
	"github.com/coze-dev/coze-studio/backend/pkg/openaiproxy"
)

func TestOpenAIModelBuilderBuildReturnsProxyError(t *testing.T) {
	t.Setenv(openaiproxy.EnvKey, "://bad proxy")

	builder := newOpenaiModelBuilder(&config.Model{
		Connection: &config.Connection{
			BaseConnInfo: &config.BaseConnectionInfo{
				APIKey: "secret",
				Model:  "gpt-4o-mini",
			},
			Openai: &config.OpenAIConnInfo{},
		},
	})

	model, err := builder.Build(context.Background(), nil)
	require.ErrorContains(t, err, openaiproxy.EnvKey)
	require.Nil(t, model)
}

func TestOpenAIModelBuilderApplyParamsMapsThinkingSwitch(t *testing.T) {
	builder := newOpenaiModelBuilder(&config.Model{})
	conf := &modelopenai.ChatModelConfig{}

	builder.applyParamsToOpenaiConfig(conf, &LLMParams{EnableThinking: ptr.Of(true)})
	require.Equal(t, modelopenai.ReasoningEffortLevelMedium, conf.ReasoningEffort)

	builder.applyParamsToOpenaiConfig(conf, &LLMParams{EnableThinking: ptr.Of(false)})
	require.Equal(t, modelopenai.ReasoningEffortLevel(""), conf.ReasoningEffort)
}
