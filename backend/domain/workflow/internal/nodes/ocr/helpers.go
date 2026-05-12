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
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/coze-dev/coze-studio/backend/pkg/urltobase64url"
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

const maxFileSize = 20 * 1024 * 1024 // 20MB

func isPrivateIP(ip net.IP) bool {
	privateRanges := []net.IPNet{
		{IP: net.IPv4(10, 0, 0, 0), Mask: net.CIDRMask(8, 32)},
		{IP: net.IPv4(172, 16, 0, 0), Mask: net.CIDRMask(12, 32)},
		{IP: net.IPv4(192, 168, 0, 0), Mask: net.CIDRMask(16, 32)},
		{IP: net.IPv4(127, 0, 0, 0), Mask: net.CIDRMask(8, 32)},
		{IP: net.IPv4(169, 254, 0, 0), Mask: net.CIDRMask(16, 32)},
	}
	for _, r := range privateRanges {
		if r.Contains(ip) {
			return true
		}
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}

func validateURLSafety(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported URL scheme: %s (only http/https allowed)", u.Scheme)
	}
	host := u.Hostname()
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("cannot resolve host %s: %w", host, err)
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("access to private/internal network address is not allowed")
		}
	}
	return nil
}

// fetchFileSecure downloads a file with context propagation, size limit,
// and SSRF protection (scheme/IP validation, no redirect to private nets).
func fetchFileSecure(ctx context.Context, fileURL string, client *http.Client) (*urltobase64url.FileData, error) {
	if err := validateURLSafety(fileURL); err != nil {
		return nil, err
	}

	safeClient := *client
	safeClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		if err := validateURLSafety(req.URL.String()); err != nil {
			return fmt.Errorf("redirect blocked: %w", err)
		}
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET", fileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid file URL: %w", err)
	}

	resp, err := safeClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("file download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("file download returned status %d", resp.StatusCode)
	}

	limitedReader := io.LimitReader(resp.Body, maxFileSize+1)
	fileContent, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	if int64(len(fileContent)) > maxFileSize {
		return nil, fmt.Errorf("file exceeds maximum size of %dMB", maxFileSize/(1024*1024))
	}

	var mimeType string
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		if mt, _, err := mime.ParseMediaType(ct); err == nil && mt != "" {
			mimeType = mt
		}
	}
	if mimeType == "" {
		mimeType = http.DetectContentType(fileContent)
	}
	if mimeType == "" || mimeType == "application/octet-stream" {
		if ext := filepath.Ext(fileURL); ext != "" {
			mimeType = mime.TypeByExtension(ext)
		}
	}

	b64 := base64.StdEncoding.EncodeToString(fileContent)
	dataURI := fmt.Sprintf("data:%s;base64,%s", mimeType, b64)

	return &urltobase64url.FileData{
		Base64Url: dataURI,
		MimeType:  mimeType,
	}, nil
}
