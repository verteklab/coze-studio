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
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExtractJSONPath(t *testing.T) {
	data := map[string]any{
		"choices": []any{
			map[string]any{
				"message": map[string]any{
					"content": "Hello OCR",
				},
			},
		},
	}

	result := extractJSONPath(data, "choices", 0, "message", "content")
	if result != "Hello OCR" {
		t.Errorf("expected 'Hello OCR', got '%s'", result)
	}

	// Test missing path
	result = extractJSONPath(data, "choices", 1, "message", "content")
	if result != "" {
		t.Errorf("expected empty string for missing path, got '%s'", result)
	}
}

func TestExtractPaddleOCRText(t *testing.T) {
	result := map[string]any{
		"result": map[string]any{
			"ocrResults": []any{
				map[string]any{
					"prunedResult": map[string]any{
						"rec_texts": []any{"line 1", "line 2", "line 3"},
					},
				},
			},
		},
	}

	text := extractPaddleOCRText(result)
	expected := "line 1\nline 2\nline 3"
	if text != expected {
		t.Errorf("expected '%s', got '%s'", expected, text)
	}

	// Test empty result
	text = extractPaddleOCRText(map[string]any{})
	if text != "" {
		t.Errorf("expected empty string for empty result, got '%s'", text)
	}
}

func TestExtractBySimpleJSONPath(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]any
		path     string
		expected string
	}{
		{
			name: "simple nested path",
			data: map[string]any{
				"result": map[string]any{
					"text": "Hello",
				},
			},
			path:     "result.text",
			expected: "Hello",
		},
		{
			name: "array index path",
			data: map[string]any{
				"choices": []any{
					map[string]any{
						"message": map[string]any{
							"content": "World",
						},
					},
				},
			},
			path:     "choices.0.message.content",
			expected: "World",
		},
		{
			name: "missing path",
			data: map[string]any{
				"foo": "bar",
			},
			path:     "baz.qux",
			expected: "",
		},
		{
			name: "array of strings joined",
			data: map[string]any{
				"texts": []any{"a", "b", "c"},
			},
			path:     "texts",
			expected: "a\nb\nc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractBySimpleJSONPath(tt.data, tt.path)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestDecodeBase64DataURI(t *testing.T) {
	// "Hello" in base64 is "SGVsbG8="
	data, err := decodeBase64DataURI("data:text/plain;base64,SGVsbG8=")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "Hello" {
		t.Errorf("expected 'Hello', got '%s'", string(data))
	}

	// Test invalid format
	_, err = decodeBase64DataURI("no-comma-here")
	if err == nil {
		t.Error("expected error for invalid format")
	}
}

func TestMimeToExt(t *testing.T) {
	tests := []struct {
		mime     string
		expected string
	}{
		{"application/pdf", ".pdf"},
		{"image/png", ".png"},
		{"image/jpeg", ".jpg"},
		{"image/jpg", ".jpg"},
		{"application/octet-stream", ".bin"},
	}
	for _, tt := range tests {
		result := mimeToExt(tt.mime)
		if result != tt.expected {
			t.Errorf("mimeToExt(%s): expected '%s', got '%s'", tt.mime, tt.expected, result)
		}
	}
}

func TestExtractFileName(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://example.com/docs/report.pdf", "report.pdf"},
		{"https://example.com/docs/report.pdf?token=abc", "report.pdf"},
		{"https://example.com/", "file"},
		{"not-a-url", "not-a-url"},
	}
	for _, tt := range tests {
		result := extractFileName(tt.url)
		if result != tt.expected {
			t.Errorf("extractFileName(%s): expected '%s', got '%s'", tt.url, tt.expected, result)
		}
	}
}

func TestSupportedMimeTypes(t *testing.T) {
	if !allSupportedMimeTypes["application/pdf"] {
		t.Error("PDF should be supported")
	}
	if !allSupportedMimeTypes["image/png"] {
		t.Error("PNG should be supported")
	}
	if !allSupportedMimeTypes["image/jpeg"] {
		t.Error("JPEG should be supported")
	}
	if allSupportedMimeTypes["text/plain"] {
		t.Error("text/plain should not be supported")
	}
	if !imageMimeTypes["image/png"] {
		t.Error("PNG should be in image types")
	}
	if imageMimeTypes["application/pdf"] {
		t.Error("PDF should not be in image-only types")
	}
}

func TestIsPublicIP(t *testing.T) {
	tests := []struct {
		name   string
		ip     string
		expect bool
	}{
		// Public IPs — should be allowed
		{"Google DNS", "8.8.8.8", true},
		{"Cloudflare DNS", "1.1.1.1", true},
		{"Random public", "203.0.114.1", true},

		// Private ranges — should be blocked
		{"10.x private", "10.0.0.1", false},
		{"172.16 private", "172.16.0.1", false},
		{"192.168 private", "192.168.1.1", false},

		// Special use — should be blocked
		{"Loopback", "127.0.0.1", false},
		{"Link-local", "169.254.1.1", false},
		{"CGNAT", "100.64.0.1", false},
		{"Benchmark", "198.18.0.1", false},
		{"TEST-NET-1", "192.0.2.1", false},
		{"TEST-NET-2", "198.51.100.1", false},
		{"TEST-NET-3", "203.0.113.1", false},
		{"Reserved", "240.0.0.1", false},
		{"Broadcast", "255.255.255.255", false},
		{"Unspecified", "0.0.0.0", false},
		{"Multicast", "224.0.0.1", false},

		// IPv6 — should be blocked
		{"IPv6 loopback", "::1", false},
		{"IPv6 link-local", "fe80::1", false},
		{"IPv6 ULA", "fc00::1", false},
		{"IPv6 multicast", "ff02::1", false},
		{"IPv6 unspecified", "::", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP: %s", tt.ip)
			}
			got := isPublicIP(ip)
			if got != tt.expect {
				t.Errorf("isPublicIP(%s) = %v, want %v", tt.ip, got, tt.expect)
			}
		})
	}
}

func TestValidateURLScheme(t *testing.T) {
	tests := []struct {
		url     string
		wantErr bool
	}{
		{"https://example.com/file.png", false},
		{"http://example.com/file.png", false},
		{"ftp://example.com/file.png", true},
		{"file:///etc/passwd", true},
		{"gopher://evil.com", true},
		{"javascript:alert(1)", true},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			err := validateURLScheme(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateURLScheme(%s) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestFetchFileSecure_PrivateIP(t *testing.T) {
	// Start a local HTTP server (will be on 127.0.0.1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("fake-image-data"))
	}))
	defer ts.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	_, err := fetchFileSecure(context.Background(), ts.URL+"/test.png", client)
	if err == nil {
		t.Error("expected error when fetching from localhost, got nil")
	}
	if !strings.Contains(err.Error(), "private") && !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("expected SSRF error message, got: %v", err)
	}
}

func TestFetchFileSecure_RedirectToPrivate(t *testing.T) {
	// Server that redirects to a private IP
	privateServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("secret"))
	}))
	defer privateServer.Close()

	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, privateServer.URL+"/secret", http.StatusFound)
	}))
	defer redirectServer.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	_, err := fetchFileSecure(context.Background(), redirectServer.URL+"/redirect", client)
	if err == nil {
		t.Error("expected error when redirect targets localhost, got nil")
	}
}
