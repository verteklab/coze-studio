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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"

	"github.com/coze-dev/coze-studio/backend/api/model/admin/config"
)

func TestCustomHTTPChatCompletionsGenerate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/chat/completions", r.URL.Path)
		require.Equal(t, "Bearer secret", r.Header.Get("Authorization"))

		var req map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Equal(t, "demo-model", req["model"])

		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": "hello world",
					},
				},
			},
		})
	}))
	defer srv.Close()

	builder, err := newCustomHTTPModelBuilder(newCustomHTTPConfig(srv.URL, &config.CustomHTTPConnInfo{
		ProtocolType: customHTTPProtocolChatCompletions,
		Method:       http.MethodPost,
		Path:         "/v1/chat/completions",
	})).Build(context.Background(), nil)
	require.NoError(t, err)

	msg, err := builder.Generate(context.Background(), []*schema.Message{schema.UserMessage("hi")})
	require.NoError(t, err)
	require.Equal(t, "hello world", msg.Content)
}

func TestCustomHTTPScoresGenerate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/scores", r.URL.Path)

		var req map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Equal(t, "latest question", req["text_1"])
		require.Equal(t, "system prompt", req["text_2"])

		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"score": 0.91,
			},
		})
	}))
	defer srv.Close()

	builder, err := newCustomHTTPModelBuilder(newCustomHTTPConfig(srv.URL, &config.CustomHTTPConnInfo{
		ProtocolType:    customHTTPProtocolScores,
		Method:          http.MethodPost,
		Path:            "/scores",
		InputMappingJSON: `{"text_1":"last_user_message","text_2":"system_message"}`,
		ResponsePath:    "data.score",
		OutputMode:      customHTTPOutputModeText,
	})).Build(context.Background(), nil)
	require.NoError(t, err)

	msg, err := builder.Generate(context.Background(), []*schema.Message{
		schema.SystemMessage("system prompt"),
		schema.UserMessage("latest question"),
	})
	require.NoError(t, err)
	require.Equal(t, "0.91", msg.Content)
}

func TestProbeCustomHTTPValidation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"status": "ok",
			},
		})
	}))
	defer srv.Close()

	err := ProbeCustomHTTP(context.Background(), newCustomHTTPConfig(srv.URL, &config.CustomHTTPConnInfo{
		ProtocolType: customHTTPProtocolScores,
		Method:       http.MethodPost,
		Path:         "/scores",
		InputMappingJSON: `{"text_1":"last_user_message"}`,
		Validation: &config.CustomHTTPValidation{
			Mode:           customHTTPValidationJSONField,
			ExpectedStatus: http.StatusOK,
			JSONPath:       "data.status",
			ExpectedEquals: "ok",
		},
	}))
	require.NoError(t, err)
}

func newCustomHTTPConfig(baseURL string, custom *config.CustomHTTPConnInfo) *config.Model {
	return &config.Model{
		Connection: &config.Connection{
			BaseConnInfo: &config.BaseConnectionInfo{
				BaseURL: baseURL,
				APIKey:  "secret",
				Model:   "demo-model",
			},
			CustomHTTP: custom,
		},
	}
}
