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
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coze-dev/coze-studio/backend/pkg/urltobase64url"
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

func TestNormalizeOpenAIVisionEndpoint(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"http://easyocr:8000/v1/chat/completions", "http://easyocr:8000"},
		{"http://127.0.0.1:17006/", "http://127.0.0.1:17006"},
		{"http://host:8000/v1", "http://host:8000"},
	}
	for _, tt := range tests {
		if got := normalizeOpenAIVisionEndpoint(tt.in); got != tt.want {
			t.Errorf("normalizeOpenAIVisionEndpoint(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestBuildRejectsLegacyOpenAIEndpointConfig(t *testing.T) {
	cfg := &Config{
		Provider: ProviderOpenAIVision,
		OpenAIVision: &OpenAIVisionConfig{
			Endpoint: "http://internal:17007",
			Model:    "deepseek-ai/DeepSeek-OCR-2",
		},
	}

	_, err := cfg.Build(context.Background(), nil)
	if err == nil {
		t.Fatal("expected legacy endpoint config to be rejected")
	}
	if !strings.Contains(err.Error(), "reselect an OCR Provider") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildPaddleOCRChatRequest(t *testing.T) {
	cfg := &OpenAIVisionConfig{
		Model:                 "paddleocr-pdf",
		PaddleOCROutputFormat: "text",
	}
	body := buildPaddleOCRChatRequest(cfg, "data:application/pdf;base64,abc")
	if body["stream"] != false {
		t.Fatalf("expected stream false, got %#v", body["stream"])
	}
	msgs, ok := body["messages"].([]map[string]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("unexpected messages: %#v", body["messages"])
	}
	if msgs[0]["content"] != "data:application/pdf;base64,abc" {
		t.Fatalf("unexpected content: %#v", msgs[0]["content"])
	}
	po, ok := body["paddleocr"].(map[string]any)
	if !ok || po["output_format"] != "text" {
		t.Fatalf("unexpected paddleocr: %#v", body["paddleocr"])
	}
}

func TestBuildOpenAIVisionImageMessageContent(t *testing.T) {
	parts := buildOpenAIVisionImageMessageContent("data:image/png;base64,abc", "read text")
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[0]["type"] != "text" || parts[0]["text"] != "read text" {
		t.Fatalf("expected text part first: %#v", parts[0])
	}
	if parts[1]["type"] != "image_url" {
		t.Fatalf("expected image_url part second: %#v", parts[1])
	}
	imageURL, ok := parts[1]["image_url"].(map[string]string)
	if !ok || imageURL["url"] != "data:image/png;base64,abc" {
		t.Fatalf("unexpected image_url: %#v", parts[1])
	}
}

func TestResolveOpenAIVisionMessageFormat_DeepSeekOCR2UsesImageNative(t *testing.T) {
	tests := []struct {
		name   string
		format string
		want   string
	}{
		{name: "default", format: "", want: "image_native"},
		{name: "legacy paddleocr default", format: "paddleocr", want: "image_native"},
		{name: "legacy parts default", format: "parts", want: "image_native"},
		{name: "explicit pdf string preserved", format: "pdf_data_url_string", want: "pdf_data_url_string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &OpenAIVisionConfig{
				Model:         "deepseek-ai/DeepSeek-OCR-2",
				MessageFormat: tt.format,
			}
			if got := resolveOpenAIVisionMessageFormat(cfg); got != tt.want {
				t.Fatalf("resolveOpenAIVisionMessageFormat = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeepSeekOCR2ProviderFormatUsesImageNativeRequest(t *testing.T) {
	cfg := &OpenAIVisionConfig{
		Model:         "deepseek-ai/DeepSeek-OCR-2",
		MessageFormat: "deepseek_ocr2_image",
		Prompt:        "OCR this",
	}

	file := &urltobase64url.FileData{
		Base64Url: "data:image/png;base64,abc",
		MimeType:  "image/png",
	}
	reqBody, err := buildOpenAIVisionRequestBody(cfg, file)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	messages := reqBody["messages"].([]map[string]any)
	content := messages[0]["content"].([]map[string]any)
	if len(content) != 2 {
		t.Fatalf("expected prompt and image_url parts, got %#v", content)
	}
	if content[1]["type"] != "image_url" {
		t.Fatalf("expected image_url part, got %#v", content[1])
	}
}

func TestImageNativeRejectsPDFWhenProviderDoesNotSupportPDF(t *testing.T) {
	cfg := &OpenAIVisionConfig{
		ProviderID:    "easyocr",
		Model:         "easyocr",
		MessageFormat: "image_native",
		SupportsPDF:   false,
	}
	file := &urltobase64url.FileData{
		Base64Url: "data:application/pdf;base64,abc",
		MimeType:  "application/pdf",
	}

	_, err := buildOpenAIVisionRequestBody(cfg, file)
	if err == nil {
		t.Fatal("expected PDF input to be rejected for image-only provider")
	}
	if !strings.Contains(err.Error(), "does not support PDF") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFilePartFormatSendsSingleFilePart(t *testing.T) {
	cfg := &OpenAIVisionConfig{
		Model:         "mineru-pdf",
		MessageFormat: "file_part",
		Prompt:        "OCR this",
	}

	file := &urltobase64url.FileData{
		Base64Url: "data:application/pdf;base64,abc",
		MimeType:  "application/pdf",
	}
	reqBody, err := buildOpenAIVisionRequestBody(cfg, file)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	messages := reqBody["messages"].([]map[string]any)
	content := messages[0]["content"].([]map[string]any)
	fileParts := 0
	for _, part := range content {
		if part["type"] == "file" {
			fileParts++
		}
		if text, ok := part["text"].(string); ok && strings.HasPrefix(text, "data:application/pdf;base64,") {
			t.Fatalf("file_part format should not duplicate PDF data URL in text: %#v", part)
		}
	}
	if fileParts != 1 {
		t.Fatalf("expected exactly one file part, got %d: %#v", fileParts, content)
	}
}

func TestInvokeMinerUAsyncTaskFlow(t *testing.T) {
	var submitted bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tasks":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected method for tasks: %s", r.Method)
			}
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("parse multipart form: %v", err)
			}
			if got := r.FormValue("backend"); got != "pipeline" {
				t.Fatalf("backend = %q, want pipeline", got)
			}
			if got := r.FormValue("parse_method"); got != "auto" {
				t.Fatalf("parse_method = %q, want auto", got)
			}
			if got := r.FormValue("return_md"); got != "true" {
				t.Fatalf("return_md = %q, want true", got)
			}
			submitted = true
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"task_id":"task-1","status":"pending","status_url":"http://127.0.0.1:17008/tasks/task-1","result_url":"http://127.0.0.1:17008/tasks/task-1/result"}`))
		case "/tasks/task-1":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"task_id":"task-1","status":"completed"}`))
		case "/tasks/task-1/result":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"md_content":"hello mineru"}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	executor := &OCRExecutor{client: ts.Client()}
	file := &urltobase64url.FileData{
		Base64Url: "data:application/pdf;base64," + base64.StdEncoding.EncodeToString([]byte("%PDF-1.4")),
		MimeType:  "application/pdf",
	}
	cfg := &OpenAIVisionConfig{
		ProviderID:    "mineru",
		Endpoint:      ts.URL,
		MessageFormat: "mineru_async_task",
	}

	text, raw, err := executor.invokeMinerUAsyncTask(context.Background(), cfg, file)
	if err != nil {
		t.Fatalf("invokeMinerUAsyncTask: %v", err)
	}
	if !submitted {
		t.Fatal("task was not submitted")
	}
	if text != "hello mineru" {
		t.Fatalf("text = %q, want hello mineru", text)
	}
	if raw["task"] == nil || raw["status"] == nil || raw["result"] == nil {
		t.Fatalf("raw response should include task/status/result: %#v", raw)
	}
}

func TestInvokeDeepSeekOCR2PDFParse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/parse/pdf" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req["model"] != "deepseek-ai/DeepSeek-OCR-2" {
			t.Fatalf("unexpected model: %#v", req["model"])
		}
		if pdf, ok := req["pdf"].(string); !ok || !strings.HasPrefix(pdf, "data:application/pdf;base64,") {
			t.Fatalf("expected top-level PDF data URL, got %#v", req["pdf"])
		}
		if req["max_pages"] != float64(1) {
			t.Fatalf("expected max_pages 1, got %#v", req["max_pages"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"model":"deepseek-ai/DeepSeek-OCR-2","object":"deepseek_ocr2.pdf_parse","markdown":"# parsed","page_count":1,"pages":[{"page":1,"content":"# parsed"}]}`))
	}))
	defer ts.Close()

	executor := &OCRExecutor{client: ts.Client()}
	cfg := &OpenAIVisionConfig{
		ProviderID:    "deepseek-ocr2-pdf",
		Endpoint:      ts.URL,
		Model:         "deepseek-ai/DeepSeek-OCR-2",
		MessageFormat: "deepseek_ocr2_pdf_parse",
		SupportsPDF:   true,
	}
	file := &urltobase64url.FileData{
		Base64Url: "data:application/pdf;base64," + base64.StdEncoding.EncodeToString([]byte("%PDF-1.4")),
		MimeType:  "application/pdf",
	}

	text, raw, err := executor.invokeDeepSeekOCR2PDFParse(context.Background(), cfg, file, 0, 1)
	if err != nil {
		t.Fatalf("invokeDeepSeekOCR2PDFParse: %v", err)
	}
	if text != "# parsed" {
		t.Fatalf("text = %q, want # parsed", text)
	}
	if raw["markdown"] != "# parsed" {
		t.Fatalf("expected markdown in raw response, got %#v", raw)
	}
}

func TestOpenAIVisionSanitization(t *testing.T) {
	msg := sanitizeUserVisibleOCRMessage(`request failed http://10.0.0.7:17007/v1 Authorization: Bearer abc`)
	if strings.Contains(msg, "10.0.0.7") || strings.Contains(msg, "abc") {
		t.Fatalf("expected sensitive values to be redacted: %s", msg)
	}

	raw := map[string]any{
		"choices":  []any{map[string]any{"message": map[string]any{"content": "ok"}}},
		"endpoint": "http://10.0.0.7:17007",
		"api_key":  "secret",
	}
	safe := sanitizeOpenAIVisionResponse(raw)
	if _, ok := safe["endpoint"]; ok {
		t.Fatal("endpoint leaked into sanitized raw response")
	}
	if _, ok := safe["api_key"]; ok {
		t.Fatal("api_key leaked into sanitized raw response")
	}
}

func TestPrepareOpenAIVisionNativeFileData_KeepsImage(t *testing.T) {
	pngB64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	data := &urltobase64url.FileData{
		Base64Url: "data:image/png;base64," + pngB64,
		MimeType:  "image/png",
	}
	out, err := prepareOpenAIVisionNativeFileData(data)
	if err != nil {
		t.Fatalf("prepareOpenAIVisionNativeFileData: %v", err)
	}
	if out.MimeType != "image/png" {
		t.Fatalf("expected image/png, got %s", out.MimeType)
	}
	if !strings.HasPrefix(out.Base64Url, "data:image/png;base64,") {
		t.Fatalf("unexpected data url: %s", out.Base64Url[:32])
	}
}

func TestBuildOpenAIVisionMessageContent(t *testing.T) {
	parts := buildOpenAIVisionMessageContent("data:application/pdf;base64,abc", "OCR this")
	if len(parts) != 3 {
		t.Fatalf("expected 3 content parts, got %d", len(parts))
	}
	if parts[0]["text"] != "data:application/pdf;base64,abc" {
		t.Fatalf("unexpected pdf text part: %#v", parts[0])
	}
	filePart, ok := parts[1]["file"].(map[string]string)
	if !ok || filePart["file_data"] != "data:application/pdf;base64,abc" {
		t.Fatalf("unexpected file part: %#v", parts[1])
	}
	if parts[2]["text"] != "OCR this" {
		t.Fatalf("unexpected prompt text part: %#v", parts[2])
	}

	onlyPDF := buildOpenAIVisionMessageContent("data:application/pdf;base64,abc", "")
	if len(onlyPDF) != 2 {
		t.Fatalf("expected 2 content parts without prompt, got %d", len(onlyPDF))
	}
}

func TestIsPDFFileData(t *testing.T) {
	pdfB64 := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4"))
	data := &urltobase64url.FileData{
		Base64Url: "data:application/octet-stream;base64," + pdfB64,
		MimeType:  "application/octet-stream",
	}
	if !isPDFFileData(data) {
		t.Fatal("expected PDF detection from magic bytes")
	}
}

func TestPrepareOpenAIVisionFileData_ImageToPDF(t *testing.T) {
	// 1x1 red PNG
	pngB64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	data := &urltobase64url.FileData{
		Base64Url: "data:image/png;base64," + pngB64,
		MimeType:  "image/png",
	}
	out, err := prepareOpenAIVisionFileData(data)
	if err != nil {
		t.Fatalf("prepareOpenAIVisionFileData: %v", err)
	}
	if out.MimeType != "application/pdf" {
		t.Fatalf("expected application/pdf, got %s", out.MimeType)
	}
	if !strings.HasPrefix(out.Base64Url, "data:application/pdf;base64,") {
		t.Fatalf("unexpected data URL prefix: %s", out.Base64Url[:40])
	}
	raw, err := decodeBase64DataURI(out.Base64Url)
	if err != nil {
		t.Fatalf("decode pdf: %v", err)
	}
	if !bytes.HasPrefix(raw, []byte("%PDF")) {
		t.Fatal("converted output is not a PDF")
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

func TestParseStorageObjectKey(t *testing.T) {
	t.Setenv("STORAGE_BUCKET", "opencoze")

	tests := []struct {
		url     string
		wantKey string
		wantOK  bool
	}{
		{
			url:     "http://localhost:8890/local_storage/opencoze/tos-cn-i-abc/uid.png?x-wf-file_name=a.png",
			wantKey: "tos-cn-i-abc/uid.png",
			wantOK:  true,
		},
		{
			url:     "http://minio:9000/opencoze/tos-cn-i-abc/uid.pdf?X-Amz-Signature=abc",
			wantKey: "tos-cn-i-abc/uid.pdf",
			wantOK:  true,
		},
		{
			url:    "https://example.com/other-bucket/key.png",
			wantOK: false,
		},
		{
			url:    "data:image/png;base64,abc",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			key, ok := parseStorageObjectKey(tt.url)
			if ok != tt.wantOK {
				t.Fatalf("parseStorageObjectKey ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && key != tt.wantKey {
				t.Fatalf("got key %q, want %q", key, tt.wantKey)
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
