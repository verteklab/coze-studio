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
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/coze-dev/coze-studio/backend/domain/workflow/entity"
	"github.com/coze-dev/coze-studio/backend/domain/workflow/entity/vo"
	"github.com/coze-dev/coze-studio/backend/domain/workflow/internal/canvas/convert"
	"github.com/coze-dev/coze-studio/backend/domain/workflow/internal/nodes"
	"github.com/coze-dev/coze-studio/backend/domain/workflow/internal/schema"
	"github.com/coze-dev/coze-studio/backend/pkg/urltobase64url"
)

// ProviderType defines the OCR provider protocol template.
type ProviderType string

const (
	ProviderOpenAIVision ProviderType = "openai_vision"
	ProviderMinerU       ProviderType = "mineru"
	ProviderPaddleOCR    ProviderType = "paddleocr"
	ProviderCustomHTTP   ProviderType = "custom"
)

const (
	defaultOCRPrompt = "请对这张图片进行OCR文字识别。提取图片中的所有文字内容，保持原始排版格式。仅输出识别到的文字，不要添加任何额外说明。"
	defaultMaxTokens = 8192
)

// OpenAIVisionConfig holds configuration for the OpenAI Vision provider.
type OpenAIVisionConfig struct {
	Endpoint  string `json:"endpoint"`
	APIKey    string `json:"api_key,omitempty"`
	Model     string `json:"model"`
	Prompt    string `json:"prompt,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
}

// MinerUConfig holds configuration for the MinerU provider.
type MinerUConfig struct {
	Endpoint string `json:"endpoint"`
}

// PaddleOCRConfig holds configuration for the PaddleOCR provider.
type PaddleOCRConfig struct {
	Endpoint string `json:"endpoint"`
}

// CustomHTTPConfig holds configuration for the custom HTTP provider.
type CustomHTTPConfig struct {
	URL          string            `json:"url"`
	Method       string            `json:"method,omitempty"`
	ContentType  string            `json:"content_type,omitempty"`
	AuthType     string            `json:"auth_type,omitempty"`
	AuthToken    string            `json:"auth_token,omitempty"`
	AuthHeader   string            `json:"auth_header,omitempty"`
	AuthValue    string            `json:"auth_value,omitempty"`
	BodyTemplate string            `json:"body_template,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	JSONPath     string            `json:"json_path,omitempty"`
}

// Config is the adaptor config for the OCR node.
type Config struct {
	Provider   ProviderType `json:"provider_type"`
	Timeout    time.Duration
	RetryTimes uint64

	// provider configs (only one will be populated based on Provider)
	OpenAIVision *OpenAIVisionConfig `json:"openai_vision,omitempty"`
	MinerU       *MinerUConfig       `json:"mineru,omitempty"`
	PaddleOCR    *PaddleOCRConfig    `json:"paddleocr,omitempty"`
	CustomHTTP   *CustomHTTPConfig   `json:"custom_http,omitempty"`
}

// Adapt converts the frontend Node to a NodeSchema.
func (c *Config) Adapt(_ context.Context, n *vo.Node, _ ...nodes.AdaptOption) (*schema.NodeSchema, error) {
	ns := &schema.NodeSchema{
		Key:     vo.NodeKey(n.ID),
		Type:    entity.NodeTypeOCR,
		Name:    n.Data.Meta.Title,
		Configs: c,
	}

	if n.Data.Inputs == nil {
		return nil, fmt.Errorf("OCR node inputs cannot be nil")
	}

	ocrNode := n.Data.Inputs.OCRNode
	if ocrNode == nil {
		return nil, fmt.Errorf("OCR node ocrConfig cannot be nil")
	}

	c.Provider = ProviderType(ocrNode.ProviderType)

	// Parse provider-specific config from the raw JSON
	switch c.Provider {
	case ProviderOpenAIVision:
		cfg := &OpenAIVisionConfig{}
		if err := json.Unmarshal(ocrNode.OCRConfig, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse OpenAI Vision config: %w", err)
		}
		c.OpenAIVision = cfg
	case ProviderMinerU:
		cfg := &MinerUConfig{}
		if err := json.Unmarshal(ocrNode.OCRConfig, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse MinerU config: %w", err)
		}
		c.MinerU = cfg
	case ProviderPaddleOCR:
		cfg := &PaddleOCRConfig{}
		if err := json.Unmarshal(ocrNode.OCRConfig, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse PaddleOCR config: %w", err)
		}
		c.PaddleOCR = cfg
	case ProviderCustomHTTP:
		cfg := &CustomHTTPConfig{}
		if err := json.Unmarshal(ocrNode.OCRConfig, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse custom HTTP config: %w", err)
		}
		c.CustomHTTP = cfg
	default:
		return nil, fmt.Errorf("unsupported OCR provider type: %s", c.Provider)
	}

	if ocrNode.Setting != nil {
		if ocrNode.Setting.Timeout > 0 {
			c.Timeout = time.Duration(ocrNode.Setting.Timeout) * time.Second
		}
		retries := ocrNode.Setting.RetryTimes
		if retries < 0 {
			retries = 0
		} else if retries > 10 {
			retries = 10
		}
		c.RetryTimes = uint64(retries)
	}

	// Set input/output types from the node definition
	if err := convert.SetInputsForNodeSchema(n, ns); err != nil {
		return nil, err
	}
	if err := convert.SetOutputTypesForNodeSchema(n, ns); err != nil {
		return nil, err
	}

	return ns, nil
}

// Build creates the OCR executor.
func (c *Config) Build(_ context.Context, _ *schema.NodeSchema, _ ...schema.BuildOption) (any, error) {
	if c.Provider == "" {
		return nil, fmt.Errorf("provider type is required")
	}

	executor := &OCRExecutor{
		provider:   c.Provider,
		retryTimes: c.RetryTimes,
	}

	client := &http.Client{}
	if c.Timeout > 0 {
		client.Timeout = c.Timeout
	} else {
		client.Timeout = 120 * time.Second
	}
	executor.client = client

	switch c.Provider {
	case ProviderOpenAIVision:
		if c.OpenAIVision == nil {
			return nil, fmt.Errorf("OpenAI Vision config is required")
		}
		executor.openaiVision = c.OpenAIVision
	case ProviderMinerU:
		if c.MinerU == nil {
			return nil, fmt.Errorf("MinerU config is required")
		}
		executor.minerU = c.MinerU
	case ProviderPaddleOCR:
		if c.PaddleOCR == nil {
			return nil, fmt.Errorf("PaddleOCR config is required")
		}
		executor.paddleOCR = c.PaddleOCR
	case ProviderCustomHTTP:
		if c.CustomHTTP == nil {
			return nil, fmt.Errorf("Custom HTTP config is required")
		}
		executor.customHTTP = c.CustomHTTP
	}

	return executor, nil
}

// OCRExecutor implements InvokableNode for OCR processing.
type OCRExecutor struct {
	client     *http.Client
	provider   ProviderType
	retryTimes uint64

	openaiVision *OpenAIVisionConfig
	minerU       *MinerUConfig
	paddleOCR    *PaddleOCRConfig
	customHTTP   *CustomHTTPConfig
}

var imageMimeTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/jpg":  true,
}

var allSupportedMimeTypes = map[string]bool{
	"application/pdf": true,
	"image/png":       true,
	"image/jpeg":      true,
	"image/jpg":       true,
}

// Invoke executes the OCR node.
func (e *OCRExecutor) Invoke(ctx context.Context, input map[string]any) (map[string]any, error) {
	// Extract file URL from input
	fileURL, ok := input["file"]
	if !ok || fileURL == nil {
		return nil, fmt.Errorf("input 'file' is required")
	}

	fileURLStr, ok := fileURL.(string)
	if !ok || fileURLStr == "" {
		return nil, fmt.Errorf("input 'file' must be a non-empty string URL")
	}

	// Optional page range
	var pageStart, pageEnd int64
	if ps, ok := input["page_start"]; ok && ps != nil {
		switch v := ps.(type) {
		case int64:
			pageStart = v
		case float64:
			pageStart = int64(v)
		}
	}
	if pe, ok := input["page_end"]; ok && pe != nil {
		switch v := pe.(type) {
		case int64:
			pageEnd = v
		case float64:
			pageEnd = int64(v)
		}
	}

	// Fetch file with context, timeout, and size limit to prevent SSRF/resource exhaustion
	fileData, err := fetchFileSecure(ctx, fileURLStr, e.client)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch and convert file: %w", err)
	}

	// Validate MIME type — OpenAI Vision only supports images (not PDF via image_url)
	if e.provider == ProviderOpenAIVision {
		if !imageMimeTypes[fileData.MimeType] {
			return nil, fmt.Errorf("OpenAI Vision provider only supports image files (PNG, JPG), got: %s. For PDF, use MinerU, PaddleOCR, or Custom HTTP provider", fileData.MimeType)
		}
	} else if !allSupportedMimeTypes[fileData.MimeType] {
		return nil, fmt.Errorf("unsupported file format: %s. Supported formats: PDF, PNG, JPG", fileData.MimeType)
	}

	// Dispatch to provider
	var text string
	var rawResponse map[string]any

	switch e.provider {
	case ProviderOpenAIVision:
		text, rawResponse, err = e.invokeOpenAIVision(ctx, fileData)
	case ProviderMinerU:
		text, rawResponse, err = e.invokeMinerU(ctx, fileURLStr, fileData, pageStart, pageEnd)
	case ProviderPaddleOCR:
		text, rawResponse, err = e.invokePaddleOCR(ctx, fileData)
	case ProviderCustomHTTP:
		text, rawResponse, err = e.invokeCustomHTTP(ctx, fileData, fileURLStr, pageStart, pageEnd)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", e.provider)
	}

	if err != nil {
		return nil, err
	}

	result := map[string]any{
		"text":         text,
		"raw_response": rawResponse,
	}

	return result, nil
}

// doWithRetry executes an HTTP request with retry logic for 5xx and 429 status codes.
// It accepts a function that creates a fresh request for each attempt, because
// http.Request bodies are consumed after Do() and cannot be reused.
func (e *OCRExecutor) doWithRetry(newReq func() (*http.Request, error)) (*http.Response, error) {
	var resp *http.Response
	var err error

	for i := uint64(0); i <= e.retryTimes; i++ {
		var req *http.Request
		req, err = newReq()
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		resp, err = e.client.Do(req)
		if err != nil {
			continue
		}
		// Retry on 5xx or 429, but keep the response on the last attempt
		if (resp.StatusCode >= 500 || resp.StatusCode == 429) && i < e.retryTimes {
			_ = resp.Body.Close()
			continue
		}
		break
	}

	return resp, err
}

// invokeOpenAIVision calls an OpenAI Vision-compatible API.
func (e *OCRExecutor) invokeOpenAIVision(ctx context.Context, fileData *urltobase64url.FileData) (string, map[string]any, error) {
	cfg := e.openaiVision

	prompt := cfg.Prompt
	if prompt == "" {
		prompt = defaultOCRPrompt
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	endpoint := strings.TrimRight(cfg.Endpoint, "/")
	url := endpoint + "/v1/chat/completions"

	reqBody := map[string]any{
		"model": cfg.Model,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type":      "image_url",
						"image_url": map[string]string{"url": fileData.Base64Url},
					},
					{
						"type": "text",
						"text": prompt,
					},
				},
			},
		},
		"max_tokens":  maxTokens,
		"temperature": 0.0,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	newReq := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if cfg.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
		}
		return req, nil
	}

	resp, err := e.doWithRetry(newReq)
	if err != nil || resp == nil {
		return "", nil, fmt.Errorf("OpenAI Vision request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return "", nil, fmt.Errorf("OpenAI Vision API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", nil, fmt.Errorf("failed to parse response JSON: %w", err)
	}

	// Extract text from $.choices[0].message.content
	text := extractJSONPath(result, "choices", 0, "message", "content")

	return text, result, nil
}

// invokeMinerU calls the MinerU file_parse API.
func (e *OCRExecutor) invokeMinerU(ctx context.Context, _ string, fileData *urltobase64url.FileData, pageStart, pageEnd int64) (string, map[string]any, error) {
	cfg := e.minerU

	endpoint := strings.TrimRight(cfg.Endpoint, "/")
	url := endpoint + "/file_parse"

	// Decode the base64 to raw bytes for file upload
	rawBytes, err := decodeBase64DataURI(fileData.Base64Url)
	if err != nil {
		return "", nil, fmt.Errorf("failed to decode file data: %w", err)
	}

	// Determine file extension from MIME type
	ext := mimeToExt(fileData.MimeType)

	// Build a function that creates a fresh multipart request for each attempt
	newReq := func() (*http.Request, error) {
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)

		part, err := writer.CreateFormFile("files", "document"+ext)
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(rawBytes); err != nil {
			return nil, err
		}

		// Add optional page range parameters
		if pageStart > 0 {
			_ = writer.WriteField("start_page_id", fmt.Sprintf("%d", pageStart))
		}
		if pageEnd > 0 {
			_ = writer.WriteField("end_page_id", fmt.Sprintf("%d", pageEnd))
		}

		_ = writer.WriteField("return_md", "true")
		_ = writer.Close()

		req, err := http.NewRequestWithContext(ctx, "POST", url, &buf)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
		return req, nil
	}

	resp, err := e.doWithRetry(newReq)
	if err != nil || resp == nil {
		return "", nil, fmt.Errorf("MinerU request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return "", nil, fmt.Errorf("MinerU API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", nil, fmt.Errorf("failed to parse response JSON: %w", err)
	}

	// MinerU returns {filename: {md_content: "..."}} - extract md_content from the first key
	text := ""
	for _, v := range result {
		if m, ok := v.(map[string]any); ok {
			if mdContent, ok := m["md_content"].(string); ok {
				text = mdContent
				break
			}
		}
	}

	return text, result, nil
}

// invokePaddleOCR calls the PaddleX Serving API.
func (e *OCRExecutor) invokePaddleOCR(ctx context.Context, fileData *urltobase64url.FileData) (string, map[string]any, error) {
	cfg := e.paddleOCR

	endpoint := strings.TrimRight(cfg.Endpoint, "/")

	// PaddleX expects raw base64, not data URI format
	rawBase64 := fileData.Base64Url
	if idx := strings.Index(rawBase64, ","); idx >= 0 {
		rawBase64 = rawBase64[idx+1:]
	}

	reqBody := map[string]any{
		"file":      rawBase64,
		"fileType":  1,
		"visualize": false,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	newReq := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	}

	resp, err := e.doWithRetry(newReq)
	if err != nil || resp == nil {
		return "", nil, fmt.Errorf("PaddleOCR request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return "", nil, fmt.Errorf("PaddleOCR API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", nil, fmt.Errorf("failed to parse response JSON: %w", err)
	}

	// Extract text from $.result.ocrResults[0].prunedResult.rec_texts
	text := extractPaddleOCRText(result)

	return text, result, nil
}

// invokeCustomHTTP calls a user-defined OCR API.
func (e *OCRExecutor) invokeCustomHTTP(ctx context.Context, fileData *urltobase64url.FileData, fileURL string, pageStart, pageEnd int64) (string, map[string]any, error) {
	cfg := e.customHTTP

	method := cfg.Method
	if method == "" {
		method = "POST"
	}

	// Extract raw base64 without the data URI prefix
	rawBase64 := fileData.Base64Url
	if idx := strings.Index(rawBase64, ","); idx >= 0 {
		rawBase64 = rawBase64[idx+1:]
	}

	// Get file name from URL
	fileName := extractFileName(fileURL)

	// Pre-compute the entire request body so retries reuse identical bytes
	var precomputedBody []byte
	var precomputedContentType string

	if cfg.ContentType == "multipart/form-data" {
		rawBytes, decErr := decodeBase64DataURI(fileData.Base64Url)
		if decErr != nil {
			return "", nil, fmt.Errorf("failed to decode file data: %w", decErr)
		}
		ext := mimeToExt(fileData.MimeType)

		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		formFileName := fileName
		if filepath.Ext(fileName) == "" {
			formFileName = fileName + ext
		}
		part, err := writer.CreateFormFile("file", formFileName)
		if err != nil {
			return "", nil, fmt.Errorf("failed to create multipart form: %w", err)
		}
		if _, err := part.Write(rawBytes); err != nil {
			return "", nil, fmt.Errorf("failed to write multipart data: %w", err)
		}
		if err := writer.Close(); err != nil {
			return "", nil, fmt.Errorf("failed to finalize multipart form: %w", err)
		}
		precomputedBody = buf.Bytes()
		precomputedContentType = writer.FormDataContentType()
	} else {
		jsonBodyStr := cfg.BodyTemplate
		jsonBodyStr = strings.ReplaceAll(jsonBodyStr, "{{file_base64}}", rawBase64)
		jsonBodyStr = strings.ReplaceAll(jsonBodyStr, "{{file_url}}", fileURL)
		jsonBodyStr = strings.ReplaceAll(jsonBodyStr, "{{file_name}}", fileName)
		jsonBodyStr = strings.ReplaceAll(jsonBodyStr, "{{page_start}}", fmt.Sprintf("%d", pageStart))
		jsonBodyStr = strings.ReplaceAll(jsonBodyStr, "{{page_end}}", fmt.Sprintf("%d", pageEnd))
		precomputedBody = []byte(jsonBodyStr)
		precomputedContentType = "application/json"
	}

	newReq := func() (*http.Request, error) {
		body := bytes.NewReader(precomputedBody)

		req, err := http.NewRequestWithContext(ctx, method, cfg.URL, body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", precomputedContentType)

		// Add custom headers
		for k, v := range cfg.Headers {
			req.Header.Set(k, v)
		}

		// Set auth
		switch cfg.AuthType {
		case "bearer":
			if cfg.AuthToken != "" {
				req.Header.Set("Authorization", "Bearer "+cfg.AuthToken)
			}
		case "custom":
			if cfg.AuthHeader != "" && cfg.AuthValue != "" {
				req.Header.Set(cfg.AuthHeader, cfg.AuthValue)
			}
		}

		return req, nil
	}

	resp, err := e.doWithRetry(newReq)
	if err != nil || resp == nil {
		return "", nil, fmt.Errorf("custom HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return "", nil, fmt.Errorf("custom HTTP API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", nil, fmt.Errorf("failed to parse response JSON: %w", err)
	}

	// Extract text using user-specified JSONPath (simple dot-notation)
	text := ""
	if cfg.JSONPath != "" {
		text = extractBySimpleJSONPath(result, cfg.JSONPath)
	}

	return text, result, nil
}
