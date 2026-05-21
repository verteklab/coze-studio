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
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/coze-dev/coze-studio/backend/domain/workflow/plugin"
	"github.com/coze-dev/coze-studio/backend/pkg/urltobase64url"
	"github.com/coze-dev/coze-studio/backend/types/consts"
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

// nonPublicRanges is the list of CIDR ranges that are NOT globally routable.
var nonPublicRanges []*net.IPNet

func init() {
	cidrs := []string{
		// IPv4
		"0.0.0.0/8",          // "This host on this network" (RFC 1122)
		"10.0.0.0/8",         // Private (RFC 1918)
		"100.64.0.0/10",      // Carrier-grade NAT (RFC 6598)
		"127.0.0.0/8",        // Loopback (RFC 1122)
		"169.254.0.0/16",     // Link-local (RFC 3927)
		"172.16.0.0/12",      // Private (RFC 1918)
		"192.0.0.0/24",       // IANA special purpose (RFC 6890)
		"192.0.2.0/24",       // Documentation TEST-NET-1 (RFC 5737)
		"192.88.99.0/24",     // 6to4 relay anycast (RFC 7526)
		"192.168.0.0/16",     // Private (RFC 1918)
		"198.18.0.0/15",      // Benchmarking (RFC 2544)
		"198.51.100.0/24",    // Documentation TEST-NET-2 (RFC 5737)
		"203.0.113.0/24",     // Documentation TEST-NET-3 (RFC 5737)
		"224.0.0.0/4",        // Multicast (RFC 5771)
		"240.0.0.0/4",        // Reserved for future use (RFC 1112)
		"255.255.255.255/32", // Limited broadcast (RFC 919)
		// IPv6
		"::/128",        // Unspecified
		"::1/128",       // Loopback
		"fc00::/7",      // Unique local (RFC 4193)
		"fe80::/10",     // Link-local (RFC 4291)
		"ff00::/8",      // Multicast (RFC 4291)
		"64:ff9b::/96",  // NAT64 (RFC 6052)
		"100::/64",      // Discard-only (RFC 6666)
		"2001:db8::/32", // Documentation (RFC 3849)
		"2001::/23",     // IETF protocol assignments (RFC 2928)
	}
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err == nil {
			nonPublicRanges = append(nonPublicRanges, ipNet)
		}
	}
}

// isPublicIP returns true only for globally routable public IP addresses.
// It blocks private, loopback, link-local, multicast, unspecified, and all
// special-use ranges (CGNAT, benchmarking, documentation, IANA reserved, etc.).
func isPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, r := range nonPublicRanges {
		if r.Contains(ip) {
			return false
		}
	}
	return !ip.IsUnspecified() && !ip.IsLoopback() && !ip.IsPrivate() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast()
}

var errNotPlatformStorageURL = errors.New("not a platform storage URL")

// fetchFile loads workflow file input: platform storage (uploaded files), data URIs,
// or external URLs with SSRF protection.
func fetchFile(ctx context.Context, fileURL string, client *http.Client) (*urltobase64url.FileData, error) {
	if strings.HasPrefix(fileURL, "data:") {
		return fileDataFromDataURI(fileURL)
	}

	if data, err := fetchFromPlatformStorage(ctx, fileURL); err == nil {
		return data, nil
	} else if !errors.Is(err, errNotPlatformStorageURL) {
		return nil, err
	}

	return fetchFileSecure(ctx, fileURL, client)
}

func fileDataFromDataURI(dataURI string) (*urltobase64url.FileData, error) {
	content, err := decodeBase64DataURI(dataURI)
	if err != nil {
		return nil, fmt.Errorf("invalid data URI: %w", err)
	}
	if int64(len(content)) > maxFileSize {
		return nil, fmt.Errorf("file exceeds maximum size of %dMB", maxFileSize/(1024*1024))
	}

	mimeType := "application/octet-stream"
	if idx := strings.Index(dataURI, "data:"); idx >= 0 {
		meta := dataURI[idx+5:]
		if semi := strings.Index(meta, ";"); semi > 0 {
			mimeType = meta[:semi]
		}
	}
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(content)
	}

	return buildFileDataFromBytes(content, mimeType, "")
}

func fetchFromPlatformStorage(ctx context.Context, fileURL string) (*urltobase64url.FileData, error) {
	objectKey, ok := parseStorageObjectKey(fileURL)
	if !ok {
		return nil, errNotPlatformStorageURL
	}

	stor := plugin.GetOSS()
	if stor == nil {
		return nil, errNotPlatformStorageURL
	}

	content, err := stor.GetObject(ctx, objectKey)
	if err != nil {
		return nil, fmt.Errorf("failed to read file from storage: %w", err)
	}
	if int64(len(content)) > maxFileSize {
		return nil, fmt.Errorf("file exceeds maximum size of %dMB", maxFileSize/(1024*1024))
	}

	mimeType := http.DetectContentType(content)
	if mimeType == "" || mimeType == "application/octet-stream" {
		if ext := filepath.Ext(objectKey); ext != "" {
			mimeType = mime.TypeByExtension(ext)
		}
	}

	return buildFileDataFromBytes(content, mimeType, objectKey)
}

// parseStorageObjectKey extracts the object key from Coze Studio upload URLs
// (MinIO presigned, /local_storage/ proxy, or /{bucket}/key paths).
func parseStorageObjectKey(fileURL string) (string, bool) {
	u, err := url.Parse(fileURL)
	if err != nil || u.Path == "" {
		return "", false
	}

	path := strings.TrimPrefix(u.Path, "/")
	if strings.HasPrefix(path, "local_storage/") {
		path = strings.TrimPrefix(path, "local_storage/")
	}

	bucket := os.Getenv(consts.StorageBucket)
	if bucket == "" {
		bucket = "opencoze"
	}

	prefix := bucket + "/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}

	objectKey := strings.TrimPrefix(path, prefix)
	if objectKey == "" {
		return "", false
	}

	return objectKey, true
}

func buildFileDataFromBytes(content []byte, mimeType, nameHint string) (*urltobase64url.FileData, error) {
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	if mimeType == "application/octet-stream" && nameHint != "" {
		if ext := filepath.Ext(nameHint); ext != "" {
			if mt := mime.TypeByExtension(ext); mt != "" {
				mimeType = mt
			}
		}
	}

	b64 := base64.StdEncoding.EncodeToString(content)
	return &urltobase64url.FileData{
		Base64Url: fmt.Sprintf("data:%s;base64,%s", mimeType, b64),
		MimeType:  mimeType,
	}, nil
}

// validateURLScheme checks that the URL uses http or https scheme only.
func validateURLScheme(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported URL scheme: %s (only http/https allowed)", u.Scheme)
	}
	return nil
}

// fetchFileSecure downloads a file with context propagation, size limit,
// and SSRF protection. DNS resolution and IP validation happen inside a
// custom DialContext to prevent DNS rebinding TOCTOU attacks.
func fetchFileSecure(ctx context.Context, fileURL string, client *http.Client) (*urltobase64url.FileData, error) {
	if err := validateURLScheme(fileURL); err != nil {
		return nil, err
	}

	safeTransport := &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("cannot resolve host %s: %w", host, err)
			}

			var safeIPs []net.IPAddr
			for _, ipAddr := range ips {
				if isPublicIP(ipAddr.IP) {
					safeIPs = append(safeIPs, ipAddr)
				}
			}
			if len(safeIPs) == 0 {
				return nil, fmt.Errorf("access to private/internal network address is not allowed")
			}

			dialer := &net.Dialer{}
			var lastErr error
			for _, ipAddr := range safeIPs {
				conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ipAddr.IP.String(), port))
				if dialErr != nil {
					lastErr = dialErr
					continue
				}
				return conn, nil
			}
			return nil, lastErr
		},
	}

	safeClient := &http.Client{
		Timeout:   client.Timeout,
		Transport: safeTransport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			if err := validateURLScheme(req.URL.String()); err != nil {
				return fmt.Errorf("redirect blocked: %w", err)
			}
			return nil
		},
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

	mimeType := ""
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

	return buildFileDataFromBytes(fileContent, mimeType, fileURL)
}
