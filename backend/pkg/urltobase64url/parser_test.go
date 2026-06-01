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

package urltobase64url

import "testing"

func TestInternalFetchURL(t *testing.T) {
	const (
		serverHost   = "http://117.59.171.81:8891"
		minioAPIHost = "http://minio:9000"
		presigned    = "?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Signature=abc&X-Amz-SignedHeaders=host"
	)

	tests := []struct {
		name       string
		serverHost string
		minioHost  string
		in         string
		want       string
	}{
		{
			name:       "rewrite public proxy url to internal minio endpoint",
			serverHost: serverHost,
			minioHost:  minioAPIHost,
			in:         serverHost + "/local_storage/opencoze/tos-cn-i/cat.png" + presigned,
			want:       minioAPIHost + "/opencoze/tos-cn-i/cat.png" + presigned,
		},
		{
			name:       "external url is left untouched",
			serverHost: serverHost,
			minioHost:  minioAPIHost,
			in:         "https://example.com/cat.png" + presigned,
			want:       "https://example.com/cat.png" + presigned,
		},
		{
			name:       "non-minio deployment (no MINIO_API_HOST) is a no-op",
			serverHost: serverHost,
			minioHost:  "",
			in:         serverHost + "/local_storage/opencoze/tos-cn-i/cat.png" + presigned,
			want:       serverHost + "/local_storage/opencoze/tos-cn-i/cat.png" + presigned,
		},
		{
			name:       "server host without local_storage prefix still rewrites host",
			serverHost: serverHost,
			minioHost:  minioAPIHost,
			in:         serverHost + "/opencoze/tos-cn-i/cat.png" + presigned,
			want:       minioAPIHost + "/opencoze/tos-cn-i/cat.png" + presigned,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SERVER_HOST", tt.serverHost)
			t.Setenv("MINIO_API_HOST", tt.minioHost)
			if got := internalFetchURL(tt.in); got != tt.want {
				t.Fatalf("internalFetchURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
