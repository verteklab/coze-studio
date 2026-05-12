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

package ocr

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// extractJSONPath extracts a nested value from a map using variadic keys.
// Supports string keys for object access and int keys for array access.
func extractJSONPath(data map[string]any, keys ...any) string {
	var current any = data
	for _, key := range keys {
		switch k := key.(type) {
		case string:
			m, ok := current.(map[string]any)
			if !ok {
				return ""
			}
			current = m[k]
		case int:
			arr, ok := current.([]any)
			if !ok || k >= len(arr) {
				return ""
			}
			current = arr[k]
		default:
			return ""
		}
	}
	if s, ok := current.(string); ok {
		return s
	}
	return ""
}

// extractPaddleOCRText extracts text from PaddleOCR response.
// Path: $.result.ocrResults[0].prunedResult.rec_texts -> join with newline.
func extractPaddleOCRText(result map[string]any) string {
	resultObj, ok := result["result"].(map[string]any)
	if !ok {
		return ""
	}

	ocrResults, ok := resultObj["ocrResults"].([]any)
	if !ok || len(ocrResults) == 0 {
		return ""
	}

	firstResult, ok := ocrResults[0].(map[string]any)
	if !ok {
		return ""
	}

	prunedResult, ok := firstResult["prunedResult"].(map[string]any)
	if !ok {
		return ""
	}

	recTexts, ok := prunedResult["rec_texts"].([]any)
	if !ok {
		return ""
	}

	var texts []string
	for _, t := range recTexts {
		if s, ok := t.(string); ok {
			texts = append(texts, s)
		}
	}

	return strings.Join(texts, "\n")
}

// extractBySimpleJSONPath extracts a value using simple dot-notation JSONPath.
// Supports paths like "choices.0.message.content" or "result.text".
func extractBySimpleJSONPath(data map[string]any, path string) string {
	parts := strings.Split(path, ".")
	var current any = data

	for _, part := range parts {
		switch v := current.(type) {
		case map[string]any:
			current = v[part]
		case []any:
			idx := 0
			for i := 0; i < len(part); i++ {
				if part[i] < '0' || part[i] > '9' {
					return ""
				}
				idx = idx*10 + int(part[i]-'0')
			}
			if idx >= len(v) {
				return ""
			}
			current = v[idx]
		default:
			return ""
		}
	}

	if current == nil {
		return ""
	}

	switch v := current.(type) {
	case string:
		return v
	case []any:
		var texts []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				texts = append(texts, s)
			}
		}
		return strings.Join(texts, "\n")
	default:
		return fmt.Sprintf("%v", current)
	}
}

// decodeBase64DataURI decodes a data URI (data:mime;base64,xxx) to raw bytes.
func decodeBase64DataURI(dataURI string) ([]byte, error) {
	idx := strings.Index(dataURI, ",")
	if idx < 0 {
		return nil, fmt.Errorf("invalid data URI format")
	}
	encoded := dataURI[idx+1:]
	return base64.StdEncoding.DecodeString(encoded)
}

// mimeToExt converts a MIME type to a file extension.
func mimeToExt(mimeType string) string {
	switch mimeType {
	case "application/pdf":
		return ".pdf"
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	default:
		return ".bin"
	}
}

// extractFileName extracts the file name from a URL.
func extractFileName(fileURL string) string {
	u, err := url.Parse(fileURL)
	if err != nil {
		return "file"
	}
	name := filepath.Base(u.Path)
	if name == "" || name == "." || name == "/" {
		return "file"
	}
	return name
}
