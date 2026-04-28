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

package openaiproxy

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewHTTPClientFromEnv(t *testing.T) {
	t.Run("empty env returns nil", func(t *testing.T) {
		t.Setenv(EnvKey, "")

		client, err := NewHTTPClientFromEnv()
		require.NoError(t, err)
		require.Nil(t, client)
	})

	t.Run("invalid env returns error", func(t *testing.T) {
		t.Setenv(EnvKey, "://bad proxy")

		client, err := NewHTTPClientFromEnv()
		require.Error(t, err)
		require.Nil(t, client)
	})

	t.Run("valid env returns proxy client", func(t *testing.T) {
		t.Setenv(EnvKey, "http://host.docker.internal:8118")

		client, err := NewHTTPClientFromEnv()
		require.NoError(t, err)
		require.NotNil(t, client)
		require.Equal(t, defaultTimeout, client.Timeout)

		transport, ok := client.Transport.(*http.Transport)
		require.True(t, ok)

		req, err := http.NewRequest(http.MethodGet, "https://api.openai.com/v1/chat/completions", nil)
		require.NoError(t, err)

		proxyURL, err := transport.Proxy(req)
		require.NoError(t, err)
		require.Equal(t, "http://host.docker.internal:8118", proxyURL.String())
		require.Equal(t, 60*time.Second, client.Timeout)
	})
}
