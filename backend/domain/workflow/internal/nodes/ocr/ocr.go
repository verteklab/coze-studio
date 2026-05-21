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
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	domainWorkflow "github.com/coze-dev/coze-studio/backend/domain/workflow"
	workflowConfig "github.com/coze-dev/coze-studio/backend/domain/workflow/config"
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
	defaultOCRPrompt              = "请对这张图片进行OCR文字识别。提取图片中的所有文字内容，保持原始排版格式。仅输出识别到的文字，不要添加任何额外说明。"
	defaultMaxTokens              = 8192
	minerUAsyncPollInterval       = 2 * time.Second
	defaultMinerUAsyncPollTimeout = 10 * time.Minute
)

// OpenAIVisionConfig holds configuration for the OpenAI Vision provider.
type OpenAIVisionConfig struct {
	ProviderID   string `json:"provider_id,omitempty"`
	Endpoint     string `json:"endpoint"`
	APIKey       string `json:"api_key,omitempty"`
	Model        string `json:"model"`
	Prompt       string `json:"prompt,omitempty"`
	MaxTokens    int    `json:"max_tokens,omitempty"`
	ResponsePath string `json:"response_path,omitempty"`
	SupportsPDF  bool   `json:"-"`
	// MessageFormat controls request shape:
	//   - "" or "parts": PDF payload as text+file parts (images converted to single-page PDF)
	//   - "pdf_data_url_string" or "string": messages[].content is the file data URL string only
	//   - "image_native" or "image": image_url+file parts, images not converted to PDF (EasyOCR-style APIs)
	//   - "paddleocr": PaddleOCR Docker wrapper — content is data:application/pdf;base64,... string, plus paddleocr.output_format
	MessageFormat         string `json:"message_format,omitempty"`
	PaddleOCROutputFormat string `json:"paddleocr_output_format,omitempty"`
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

	if ocrNode.OCRSetting != nil {
		if ocrNode.OCRSetting.Timeout > 0 {
			c.Timeout = time.Duration(ocrNode.OCRSetting.Timeout) * time.Second
		}
		retries := ocrNode.OCRSetting.RetryTimes
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
		cfg, err := resolveOpenAIVisionProvider(c.OpenAIVision)
		if err != nil {
			return nil, err
		}
		executor.openaiVision = cfg
	case ProviderMinerU:
		return nil, fmt.Errorf("legacy OCR provider config is no longer supported; please reselect an OCR Provider")
	case ProviderPaddleOCR:
		return nil, fmt.Errorf("legacy OCR provider config is no longer supported; please reselect an OCR Provider")
	case ProviderCustomHTTP:
		return nil, fmt.Errorf("legacy OCR provider config is no longer supported; please reselect an OCR Provider")
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
	fileData, err := fetchFile(ctx, fileURLStr, e.client)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch and convert file: %w", err)
	}

	if e.provider == ProviderOpenAIVision {
		if !allSupportedMimeTypes[fileData.MimeType] && !isPDFFileData(fileData) {
			return nil, fmt.Errorf("unsupported file format for OpenAI Vision: %s. Supported: PDF, PNG, JPG", fileData.MimeType)
		}
	} else if !allSupportedMimeTypes[fileData.MimeType] {
		return nil, fmt.Errorf("unsupported file format: %s. Supported formats: PDF, PNG, JPG", fileData.MimeType)
	}

	// Dispatch to provider
	var text string
	var rawResponse map[string]any

	switch e.provider {
	case ProviderOpenAIVision:
		text, rawResponse, err = e.invokeOpenAIVision(ctx, fileData, pageStart, pageEnd)
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

	if rawResponse == nil {
		rawResponse = map[string]any{}
	}
	rawResponse["preview"] = text

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
func (e *OCRExecutor) invokeOpenAIVision(ctx context.Context, fileData *urltobase64url.FileData, pageStart, pageEnd int64) (string, map[string]any, error) {
	cfg := e.openaiVision

	if resolveOpenAIVisionMessageFormat(cfg) == "mineru_async_task" {
		return e.invokeMinerUAsyncTask(ctx, cfg, fileData)
	}
	if resolveOpenAIVisionMessageFormat(cfg) == "deepseek_ocr2_pdf_parse" {
		return e.invokeDeepSeekOCR2PDFParse(ctx, cfg, fileData, pageStart, pageEnd)
	}

	endpoint := normalizeOpenAIVisionEndpoint(cfg.Endpoint)
	url := endpoint + "/v1/chat/completions"

	reqBody, err := buildOpenAIVisionRequestBody(cfg, fileData)
	if err != nil {
		return "", nil, err
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
		return "", nil, formatOpenAIVisionAPIError(cfg, resp.StatusCode, respBody)
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", nil, fmt.Errorf("failed to parse response JSON: %w", err)
	}

	// Extract text from $.choices[0].message.content
	text := extractOpenAIVisionText(result, cfg.ResponsePath)

	return text, sanitizeOpenAIVisionResponse(result), nil
}

func (e *OCRExecutor) invokeDeepSeekOCR2PDFParse(ctx context.Context, cfg *OpenAIVisionConfig, fileData *urltobase64url.FileData, _, pageEnd int64) (string, map[string]any, error) {
	pdfFileData, err := prepareOpenAIVisionFileData(fileData)
	if err != nil {
		return "", nil, err
	}
	reqBody := map[string]any{
		"model": cfg.Model,
		"pdf":   pdfFileData.Base64Url,
	}
	if pageEnd > 0 {
		reqBody["max_pages"] = pageEnd
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal DeepSeek OCR2 PDF parse request body: %w", err)
	}

	endpoint := normalizeOpenAIVisionEndpoint(cfg.Endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/v1/parse/pdf", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	resp, err := e.client.Do(req)
	if err != nil || resp == nil {
		return "", nil, fmt.Errorf("DeepSeek OCR2 PDF parse request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read DeepSeek OCR2 PDF parse response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", nil, formatOpenAIVisionAPIError(cfg, resp.StatusCode, respBody)
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", nil, fmt.Errorf("failed to parse DeepSeek OCR2 PDF parse response JSON: %w", err)
	}
	text := extractDeepSeekOCR2PDFText(result)
	return text, sanitizeDeepSeekOCR2PDFResponse(result), nil
}

func buildOpenAIVisionRequestBody(cfg *OpenAIVisionConfig, fileData *urltobase64url.FileData) (map[string]any, error) {
	prompt := cfg.Prompt
	if prompt == "" {
		prompt = defaultOCRPrompt
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	format := resolveOpenAIVisionMessageFormat(cfg)
	switch format {
	case "paddleocr", "paddleocr_chat":
		pdfFileData, err := prepareOpenAIVisionFileData(fileData)
		if err != nil {
			return nil, err
		}
		return buildPaddleOCRChatRequest(cfg, pdfFileData.Base64Url), nil
	case "deepseek_ocr2_image", "image_native", "image":
		if isPDFFileData(fileData) && !cfg.SupportsPDF {
			return nil, fmt.Errorf("OCR provider %q does not support PDF input; please use paddle-ocr or mineru for PDF files", cfg.ProviderID)
		}
		nativeFileData, err := prepareOpenAIVisionNativeFileData(fileData)
		if err != nil {
			return nil, err
		}
		messageContent := buildOpenAIVisionImageMessageContent(nativeFileData.Base64Url, prompt)
		return map[string]any{
			"model": cfg.Model,
			"messages": []map[string]any{
				{"role": "user", "content": messageContent},
			},
			"max_tokens":  maxTokens,
			"stream":      false,
			"temperature": 0.0,
		}, nil
	case "pdf_data_url_string", "string":
		pdfFileData, err := prepareOpenAIVisionFileData(fileData)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"model": cfg.Model,
			"messages": []map[string]any{
				{"role": "user", "content": pdfFileData.Base64Url},
			},
			"max_tokens":  maxTokens,
			"stream":      false,
			"temperature": 0.0,
		}, nil
	case "file_part", "single_file":
		pdfFileData, err := prepareOpenAIVisionFileData(fileData)
		if err != nil {
			return nil, err
		}
		messageContent := buildOpenAIVisionFileMessageContent(pdfFileData.Base64Url, prompt)
		return map[string]any{
			"model": cfg.Model,
			"messages": []map[string]any{
				{"role": "user", "content": messageContent},
			},
			"max_tokens":  maxTokens,
			"stream":      false,
			"temperature": 0.0,
		}, nil
	default:
		pdfFileData, err := prepareOpenAIVisionFileData(fileData)
		if err != nil {
			return nil, err
		}
		messageContent := buildOpenAIVisionMessageContent(pdfFileData.Base64Url, prompt)
		return map[string]any{
			"model": cfg.Model,
			"messages": []map[string]any{
				{"role": "user", "content": messageContent},
			},
			"max_tokens":  maxTokens,
			"stream":      false,
			"temperature": 0.0,
		}, nil
	}
}

func resolveOpenAIVisionProvider(nodeCfg *OpenAIVisionConfig) (*OpenAIVisionConfig, error) {
	providerID := strings.TrimSpace(nodeCfg.ProviderID)
	if providerID == "" {
		return nil, fmt.Errorf("legacy OCR endpoint config is no longer supported; please reselect an OCR Provider")
	}

	provider, err := domainWorkflow.GetRepository().GetOCRProviderByID(providerID)
	if err != nil {
		return nil, err
	}
	if err := validateOCRProviderEndpoint(provider); err != nil {
		return nil, err
	}

	cfg := &OpenAIVisionConfig{
		ProviderID:            provider.ID,
		Endpoint:              provider.Endpoint,
		APIKey:                provider.APIKey,
		Model:                 provider.Model,
		Prompt:                nodeCfg.Prompt,
		MaxTokens:             nodeCfg.MaxTokens,
		MessageFormat:         provider.Format,
		PaddleOCROutputFormat: nodeCfg.PaddleOCROutputFormat,
		ResponsePath:          provider.ResponsePath,
		SupportsPDF:           provider.Capabilities != nil && provider.Capabilities.SupportsPDF,
	}
	if cfg.PaddleOCROutputFormat == "" {
		cfg.PaddleOCROutputFormat = "text"
	}
	return cfg, nil
}

func validateOCRProviderEndpoint(provider *workflowConfig.OCRProvider) error {
	endpoint, err := url.Parse(strings.TrimSpace(provider.Endpoint))
	if err != nil || endpoint.Scheme == "" || endpoint.Hostname() == "" {
		return fmt.Errorf("invalid OCR provider endpoint for %s", provider.ID)
	}
	if len(provider.AllowedHosts) == 0 {
		return nil
	}
	host := endpoint.Hostname()
	for _, allowed := range provider.AllowedHosts {
		if strings.EqualFold(strings.TrimSpace(allowed), host) {
			return nil
		}
	}
	return fmt.Errorf("OCR provider %s endpoint host is not allowed", provider.ID)
}

func extractOpenAIVisionText(result map[string]any, responsePath string) string {
	if strings.TrimSpace(responsePath) != "" {
		if text := extractBySimpleJSONPath(result, responsePath); text != "" {
			return text
		}
	}
	return extractJSONPath(result, "choices", 0, "message", "content")
}

func sanitizeOpenAIVisionResponse(result map[string]any) map[string]any {
	safe := map[string]any{}
	for _, key := range []string{"id", "object", "created", "model", "usage"} {
		if value, ok := result[key]; ok {
			safe[key] = value
		}
	}
	if choices, ok := result["choices"]; ok {
		safe["choices"] = choices
	}
	return safe
}

func extractDeepSeekOCR2PDFText(result map[string]any) string {
	if text := extractBySimpleJSONPath(result, "markdown"); strings.TrimSpace(text) != "" {
		return text
	}
	if text := extractBySimpleJSONPath(result, "pages.0.content"); strings.TrimSpace(text) != "" {
		return text
	}
	return findStringByKeys(result, map[string]bool{
		"markdown": true,
		"content":  true,
		"text":     true,
	})
}

func sanitizeDeepSeekOCR2PDFResponse(result map[string]any) map[string]any {
	safe := map[string]any{}
	for _, key := range []string{"model", "object", "page_count", "pages", "markdown"} {
		if value, ok := result[key]; ok {
			safe[key] = value
		}
	}
	return safe
}

func formatOpenAIVisionAPIError(cfg *OpenAIVisionConfig, statusCode int, respBody []byte) error {
	msg := sanitizeUserVisibleOCRMessage(string(respBody))
	format := resolveOpenAIVisionMessageFormat(cfg)
	if strings.Contains(msg, "Device string must not be empty") {
		msg += " (MinerU backend device config is empty; set a valid device such as cuda/cpu in the MinerU service configuration)"
	} else if (format == "paddleocr" || format == "paddleocr_chat") &&
		(strings.Contains(msg, "hybrid-auto-engine") || strings.Contains(msg, `"task_id"`)) {
		msg += " (Endpoint may point to MinerU instead of PaddleOCR chat — use the PaddleOCR port, e.g. :17007, model paddleocr-pdf)"
	}
	return fmt.Errorf("OCR API error (provider %s, format %s, status %d): %s", cfg.ProviderID, format, statusCode, msg)
}

var sensitiveOCRValuePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)https?://[^\s"']+`),
	regexp.MustCompile(`(?i)bearer\s+[^,\s"']+`),
	regexp.MustCompile(`(?i)(api[_-]?key|authorization|bearer)\s*[:=]\s*[^,\s"']+`),
}

func sanitizeUserVisibleOCRMessage(msg string) string {
	for _, pattern := range sensitiveOCRValuePatterns {
		msg = pattern.ReplaceAllString(msg, "[redacted]")
	}
	return msg
}

func resolveOpenAIVisionMessageFormat(cfg *OpenAIVisionConfig) string {
	format := strings.ToLower(strings.TrimSpace(cfg.MessageFormat))
	if isDeepSeekOCR2Model(cfg.Model) {
		switch format {
		case "", "parts", "paddleocr", "paddleocr_chat":
			return "image_native"
		}
	}
	return format
}

func isDeepSeekOCR2Model(model string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(model, "_", "-"))
	return strings.Contains(normalized, "deepseek-ocr-2")
}

func (e *OCRExecutor) invokeMinerUAsyncTask(ctx context.Context, cfg *OpenAIVisionConfig, fileData *urltobase64url.FileData) (string, map[string]any, error) {
	endpoint := strings.TrimRight(cfg.Endpoint, "/")
	rawBytes, err := decodeBase64DataURI(fileData.Base64Url)
	if err != nil {
		return "", nil, fmt.Errorf("failed to decode file data: %w", err)
	}

	task, err := e.submitMinerUAsyncTask(ctx, endpoint, rawBytes, mimeToExt(fileData.MimeType))
	if err != nil {
		return "", nil, err
	}
	taskID, ok := task["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return "", task, fmt.Errorf("MinerU async task response missing task_id")
	}

	status, err := e.pollMinerUAsyncTask(ctx, endpoint, taskID)
	if err != nil {
		return "", status, err
	}

	result, err := e.fetchMinerUAsyncResult(ctx, endpoint, taskID)
	if err != nil {
		return "", status, err
	}

	text := extractMinerUAsyncText(result)
	raw := map[string]any{
		"task":   task,
		"status": status,
		"result": result,
	}
	return text, raw, nil
}

func (e *OCRExecutor) submitMinerUAsyncTask(ctx context.Context, endpoint string, rawBytes []byte, ext string) (map[string]any, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("files", "document"+ext)
	if err != nil {
		return nil, fmt.Errorf("failed to create MinerU task form: %w", err)
	}
	if _, err := part.Write(rawBytes); err != nil {
		return nil, fmt.Errorf("failed to write MinerU task file: %w", err)
	}
	_ = writer.WriteField("backend", "pipeline")
	_ = writer.WriteField("parse_method", "auto")
	_ = writer.WriteField("return_md", "true")
	_ = writer.WriteField("return_content_list", "true")
	_ = writer.WriteField("return_images", "false")
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize MinerU task form: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/tasks", &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	return e.doMinerUJSONRequest(req, "submit MinerU async task")
}

func (e *OCRExecutor) pollMinerUAsyncTask(ctx context.Context, endpoint, taskID string) (map[string]any, error) {
	pollTimeout := defaultMinerUAsyncPollTimeout
	if e.client != nil && e.client.Timeout > pollTimeout {
		pollTimeout = e.client.Timeout
	}
	deadline := time.NewTimer(pollTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(minerUAsyncPollInterval)
	defer ticker.Stop()

	for {
		status, err := e.getMinerUAsyncTaskStatus(ctx, endpoint, taskID)
		if err != nil {
			return status, err
		}
		switch strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", status["status"]))) {
		case "completed", "success", "succeeded", "done":
			return status, nil
		case "failed", "error":
			return status, fmt.Errorf("MinerU async task failed: %s", sanitizeUserVisibleOCRMessage(convMapToJSON(status)))
		}

		select {
		case <-ctx.Done():
			return status, ctx.Err()
		case <-deadline.C:
			return status, fmt.Errorf("MinerU async task %s timed out after %s; last status: %s", taskID, pollTimeout, sanitizeUserVisibleOCRMessage(convMapToJSON(status)))
		case <-ticker.C:
		}
	}
}

func (e *OCRExecutor) getMinerUAsyncTaskStatus(ctx context.Context, endpoint, taskID string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/tasks/"+url.PathEscape(taskID), nil)
	if err != nil {
		return nil, err
	}
	return e.doMinerUJSONRequest(req, "poll MinerU async task")
}

func (e *OCRExecutor) fetchMinerUAsyncResult(ctx context.Context, endpoint, taskID string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/tasks/"+url.PathEscape(taskID)+"/result", nil)
	if err != nil {
		return nil, err
	}
	return e.doMinerUJSONRequest(req, "fetch MinerU async result")
}

func (e *OCRExecutor) doMinerUJSONRequest(req *http.Request, action string) (map[string]any, error) {
	resp, err := e.client.Do(req)
	if err != nil || resp == nil {
		return nil, fmt.Errorf("%s failed: %w", action, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read MinerU response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s API error (status %d): %s", action, resp.StatusCode, sanitizeUserVisibleOCRMessage(string(respBody)))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse MinerU response JSON: %w", err)
	}
	return result, nil
}

func extractMinerUAsyncText(result map[string]any) string {
	for _, path := range []string{
		"md_content",
		"markdown",
		"content",
		"text",
		"result.md_content",
		"result.markdown",
		"data.md_content",
		"data.markdown",
	} {
		if text := extractBySimpleJSONPath(result, path); strings.TrimSpace(text) != "" {
			return text
		}
	}
	return findStringByKeys(result, map[string]bool{
		"md_content": true,
		"markdown":   true,
		"content":    true,
		"text":       true,
	})
}

func findStringByKeys(value any, keys map[string]bool) string {
	switch v := value.(type) {
	case map[string]any:
		for key, nested := range v {
			if keys[key] {
				if text, ok := nested.(string); ok && strings.TrimSpace(text) != "" {
					return text
				}
			}
			if text := findStringByKeys(nested, keys); text != "" {
				return text
			}
		}
	case []any:
		for _, nested := range v {
			if text := findStringByKeys(nested, keys); text != "" {
				return text
			}
		}
	}
	return ""
}

func convMapToJSON(value map[string]any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(raw)
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
