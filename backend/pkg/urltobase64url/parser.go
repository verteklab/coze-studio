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

import (
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type FileData struct {
	Base64Url string
	MimeType  string
}

// localStorageProxyPrefix is the path prefix that the SERVER_HOST reverse proxy
// (nginx) uses to expose the MinIO bucket to browsers. Object URLs handed to the
// frontend look like `{SERVER_HOST}/local_storage/{bucket}/{key}?{presigned}`,
// while MinIO itself serves them at `{MINIO_API_HOST}/{bucket}/{key}?{presigned}`.
const localStorageProxyPrefix = "/local_storage"

// internalFetchURL rewrites a public object-storage URL (served through the
// SERVER_HOST reverse proxy) to the internal MinIO endpoint so the backend can
// fetch it from inside the container network. In containerized deployments the
// public SERVER_HOST (e.g. an external IP) is frequently unreachable from the
// backend container, which breaks server-side base64 inlining of images. The
// presigned signature is computed against the MinIO endpoint host, so swapping
// the host back to MINIO_API_HOST keeps the signature valid. URLs that do not
// originate from SERVER_HOST (e.g. user-provided external images) and non-MinIO
// deployments (MINIO_API_HOST unset) are returned unchanged.
func internalFetchURL(rawURL string) string {
	serverHost := strings.TrimRight(os.Getenv("SERVER_HOST"), "/")
	internalHost := strings.TrimRight(os.Getenv("MINIO_API_HOST"), "/")
	if serverHost == "" || internalHost == "" {
		return rawURL
	}
	if !strings.HasPrefix(rawURL, serverHost) {
		return rawURL
	}

	rest := strings.TrimPrefix(rawURL, serverHost)
	rest = strings.TrimPrefix(rest, localStorageProxyPrefix)
	if !strings.HasPrefix(rest, "/") {
		rest = "/" + rest
	}
	return internalHost + rest
}

func URLToBase64(url string) (*FileData, error) {

	resp, err := http.Get(internalFetchURL(url))
	if err != nil {
		return nil, fmt.Errorf("http get error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("response status code error: %d", resp.StatusCode)
	}

	fileContent, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read file content error: %v", err)
	}

	var mimeType string

	contentType := resp.Header.Get("Content-Type")
	if contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err == nil && mediaType != "" {
			mimeType = mediaType
		}
	}

	if mimeType == "" {
		detectedType := http.DetectContentType(fileContent)
		if detectedType != "application/octet-stream" {
			mimeType = detectedType
		}
	}

	if mimeType == "" || mimeType == "application/octet-stream" {
		urlPath := url
		if idx := strings.Index(urlPath, "?"); idx != -1 {
			urlPath = urlPath[:idx]
		}
		if idx := strings.Index(urlPath, "#"); idx != -1 {
			urlPath = urlPath[:idx]
		}

		ext := filepath.Ext(urlPath)
		if ext != "" {
			extMimeType := mime.TypeByExtension(ext)
			if extMimeType != "" {
				mimeType = extMimeType
			}
		}
	}

	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	base64Str := base64.StdEncoding.EncodeToString(fileContent)

	return &FileData{
		Base64Url: "data:" + mimeType + ";base64," + base64Str,
		MimeType:  mimeType,
	}, nil
}
