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
	"testing"
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
	if !supportedMimeTypes["application/pdf"] {
		t.Error("PDF should be supported")
	}
	if !supportedMimeTypes["image/png"] {
		t.Error("PNG should be supported")
	}
	if !supportedMimeTypes["image/jpeg"] {
		t.Error("JPEG should be supported")
	}
	if supportedMimeTypes["text/plain"] {
		t.Error("text/plain should not be supported")
	}
}
