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

package modelbuilder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/coze-dev/coze-studio/backend/api/model/admin/config"
	"github.com/coze-dev/coze-studio/backend/api/model/app/bot_common"
)

const (
	customHTTPProtocolChatCompletions = "chat_completions"
	customHTTPProtocolScores          = "scores"

	customHTTPValidationStatusOnly = "status_only"
	customHTTPValidationJSONField  = "json_field"

	customHTTPOutputModeText = "text"
	customHTTPOutputModeJSON = "json"
)

var customHTTPTemplatePattern = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}`)

type customHTTPModelBuilder struct {
	cfg *config.Model
}

func newCustomHTTPModelBuilder(cfg *config.Model) Service {
	return &customHTTPModelBuilder{cfg: cfg}
}

func ProbeCustomHTTP(ctx context.Context, cfg *config.Model) error {
	rt, err := newCustomHTTPRuntime(cfg, nil)
	if err != nil {
		return err
	}

	resp, err := rt.doRequest(ctx, sampleProbeMessages())
	if err != nil {
		return err
	}

	if err := rt.validateResponse(resp); err != nil {
		return err
	}

	_, err = rt.responseToContent(resp)
	return err
}

func (b *customHTTPModelBuilder) Build(_ context.Context, params *LLMParams) (ToolCallingChatModel, error) {
	rt, err := newCustomHTTPRuntime(b.cfg, params)
	if err != nil {
		return nil, err
	}

	return &customHTTPModel{
		runtime: rt,
	}, nil
}

type customHTTPRuntime struct {
	cfg           *config.Model
	params        *LLMParams
	protocolType  string
	method        string
	path          string
	authHeader    string
	headers       map[string]string
	payloadTpl    string
	inputMapping  map[string]string
	outputMode    string
	responsePath  string
	validation    *config.CustomHTTPValidation
	client        *http.Client
}

type customHTTPResponse struct {
	StatusCode int
	Body       []byte
	JSON       any
}

type customHTTPModel struct {
	runtime *customHTTPRuntime
}

func (m *customHTTPModel) Generate(ctx context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	resp, err := m.runtime.doRequest(ctx, input)
	if err != nil {
		return nil, err
	}

	content, err := m.runtime.responseToContent(resp)
	if err != nil {
		return nil, err
	}

	return &schema.Message{
		Role:    schema.Assistant,
		Content: content,
	}, nil
}

func (m *customHTTPModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}

	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

func (m *customHTTPModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return nil, errors.New("custom HTTP model does not support tools")
}

func (m *customHTTPModel) IsCallbacksEnabled() bool {
	return true
}

func newCustomHTTPRuntime(cfg *config.Model, params *LLMParams) (*customHTTPRuntime, error) {
	if cfg == nil || cfg.Connection == nil || cfg.Connection.BaseConnInfo == nil {
		return nil, fmt.Errorf("custom HTTP model config is incomplete")
	}

	custom := cfg.Connection.CustomHTTP
	if custom == nil {
		return nil, fmt.Errorf("custom HTTP connection config is nil")
	}

	headers := map[string]string{}
	if strings.TrimSpace(custom.HeadersJSON) != "" {
		if err := json.Unmarshal([]byte(custom.HeadersJSON), &headers); err != nil {
			return nil, fmt.Errorf("parse custom HTTP headers failed: %w", err)
		}
	}

	inputMapping := map[string]string{}
	if strings.TrimSpace(custom.InputMappingJSON) != "" {
		if err := json.Unmarshal([]byte(custom.InputMappingJSON), &inputMapping); err != nil {
			return nil, fmt.Errorf("parse custom HTTP input mapping failed: %w", err)
		}
	}

	protocolType := strings.TrimSpace(custom.ProtocolType)
	if protocolType == "" {
		return nil, fmt.Errorf("custom HTTP protocol type is required")
	}

	switch protocolType {
	case customHTTPProtocolChatCompletions, customHTTPProtocolScores:
	default:
		return nil, fmt.Errorf("unsupported custom HTTP protocol type: %s", protocolType)
	}

	method := strings.ToUpper(strings.TrimSpace(custom.Method))
	if method == "" {
		method = http.MethodPost
	}

	outputMode := strings.TrimSpace(custom.OutputMode)
	if outputMode == "" {
		outputMode = customHTTPOutputModeText
	}

	validation := custom.Validation
	if validation == nil {
		validation = &config.CustomHTTPValidation{
			Mode:           customHTTPValidationStatusOnly,
			ExpectedStatus: http.StatusOK,
		}
	}
	if validation.ExpectedStatus == 0 {
		validation.ExpectedStatus = http.StatusOK
	}
	if validation.Mode == "" {
		validation.Mode = customHTTPValidationStatusOnly
	}

	return &customHTTPRuntime{
		cfg:          cfg,
		params:       params,
		protocolType: protocolType,
		method:       method,
		path:         strings.TrimSpace(custom.Path),
		authHeader:   strings.TrimSpace(custom.AuthHeader),
		headers:      headers,
		payloadTpl:   custom.PayloadTemplate,
		inputMapping: inputMapping,
		outputMode:   outputMode,
		responsePath: strings.TrimSpace(custom.ResponsePath),
		validation:   validation,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

func (r *customHTTPRuntime) doRequest(ctx context.Context, messages []*schema.Message) (*customHTTPResponse, error) {
	reqURL, err := joinCustomHTTPURL(r.cfg.Connection.BaseConnInfo.BaseURL, r.path)
	if err != nil {
		return nil, err
	}

	templateVars := r.buildTemplateVars(messages)
	body, err := r.buildRequestBody(templateVars)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, r.method, reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	for k, v := range r.headers {
		req.Header.Set(k, renderStringTemplate(v, templateVars))
	}

	apiKey := strings.TrimSpace(r.cfg.Connection.BaseConnInfo.APIKey)
	if apiKey != "" {
		authHeader := r.authHeader
		if authHeader == "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		} else {
			req.Header.Set(authHeader, apiKey)
		}
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	out := &customHTTPResponse{
		StatusCode: resp.StatusCode,
		Body:       respBody,
	}

	if len(respBody) > 0 {
		var jsonBody any
		if err := json.Unmarshal(respBody, &jsonBody); err == nil {
			out.JSON = jsonBody
		}
	}

	return out, nil
}

func (r *customHTTPRuntime) validateResponse(resp *customHTTPResponse) error {
	if resp.StatusCode != int(r.validation.ExpectedStatus) {
		return fmt.Errorf("custom HTTP probe failed: expected status %d, got %d", r.validation.ExpectedStatus, resp.StatusCode)
	}

	if r.validation.Mode != customHTTPValidationJSONField {
		return nil
	}

	if resp.JSON == nil {
		return errors.New("custom HTTP probe failed: response is not valid JSON")
	}

	v, ok := jsonLookup(resp.JSON, r.validation.JSONPath)
	if !ok {
		return fmt.Errorf("custom HTTP probe failed: json path %q not found", r.validation.JSONPath)
	}

	if r.validation.ExpectedNonEmpty && isEmptyJSONValue(v) {
		return fmt.Errorf("custom HTTP probe failed: json path %q is empty", r.validation.JSONPath)
	}

	if r.validation.ExpectedEquals != "" && stringifyJSONValue(v) != r.validation.ExpectedEquals {
		return fmt.Errorf("custom HTTP probe failed: json path %q expected %q, got %q", r.validation.JSONPath, r.validation.ExpectedEquals, stringifyJSONValue(v))
	}

	return nil
}

func (r *customHTTPRuntime) responseToContent(resp *customHTTPResponse) (string, error) {
	if r.protocolType == customHTTPProtocolChatCompletions {
		path := r.responsePath
		if path == "" {
			path = "choices.0.message.content"
		}
		if resp.JSON == nil {
			return "", errors.New("chat completions response is not valid JSON")
		}
		v, ok := jsonLookup(resp.JSON, path)
		if !ok {
			return "", fmt.Errorf("chat completions response path %q not found", path)
		}
		return stringifyJSONValue(v), nil
	}

	if r.outputMode == customHTTPOutputModeJSON {
		if resp.JSON == nil {
			return string(resp.Body), nil
		}
		if r.responsePath == "" {
			return string(resp.Body), nil
		}
		v, ok := jsonLookup(resp.JSON, r.responsePath)
		if !ok {
			return "", fmt.Errorf("scores response path %q not found", r.responsePath)
		}
		buf, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(buf), nil
	}

	if resp.JSON != nil && r.responsePath != "" {
		v, ok := jsonLookup(resp.JSON, r.responsePath)
		if !ok {
			return "", fmt.Errorf("scores response path %q not found", r.responsePath)
		}
		return stringifyJSONValue(v), nil
	}

	return string(resp.Body), nil
}

func (r *customHTTPRuntime) buildRequestBody(templateVars map[string]any) ([]byte, error) {
	if strings.TrimSpace(r.payloadTpl) != "" {
		rendered, err := renderJSONTemplate(r.payloadTpl, templateVars)
		if err != nil {
			return nil, err
		}
		return []byte(rendered), nil
	}

	body := map[string]any{
		"model": r.cfg.Connection.BaseConnInfo.Model,
	}

	if r.protocolType == customHTTPProtocolChatCompletions {
		body["messages"] = templateVars["messages"]
		if r.params != nil {
			if r.params.Temperature != nil {
				body["temperature"] = *r.params.Temperature
			}
			if r.params.MaxTokens != 0 {
				body["max_tokens"] = r.params.MaxTokens
			}
			if r.params.TopP != nil && *r.params.TopP != 0 {
				body["top_p"] = *r.params.TopP
			}
			if r.params.FrequencyPenalty != 0 {
				body["frequency_penalty"] = r.params.FrequencyPenalty
			}
			if r.params.PresencePenalty != 0 {
				body["presence_penalty"] = r.params.PresencePenalty
			}
			if r.params.ResponseFormat == bot_common.ModelResponseFormat_JSON {
				body["response_format"] = map[string]any{"type": "json_object"}
			}
		}
	} else {
		if len(r.inputMapping) == 0 {
			return nil, errors.New("scores protocol requires input_mapping_json or payload_template")
		}
		for key := range r.inputMapping {
			body[key] = templateVars[key]
		}
	}

	return json.Marshal(body)
}

func (r *customHTTPRuntime) buildTemplateVars(messages []*schema.Message) map[string]any {
	messagePayload := make([]map[string]any, 0, len(messages))
	lastUserMessage := ""
	lastMessage := ""
	systemMessages := make([]string, 0)
	allMessagesText := make([]string, 0, len(messages))

	for _, msg := range messages {
		if msg == nil {
			continue
		}
		messagePayload = append(messagePayload, map[string]any{
			"role":    string(msg.Role),
			"content": msg.Content,
		})
		if msg.Content != "" {
			lastMessage = msg.Content
			allMessagesText = append(allMessagesText, fmt.Sprintf("%s: %s", msg.Role, msg.Content))
		}
		if msg.Role == schema.User && msg.Content != "" {
			lastUserMessage = msg.Content
		}
		if msg.Role == schema.System && msg.Content != "" {
			systemMessages = append(systemMessages, msg.Content)
		}
	}

	vars := map[string]any{
		"model":             r.cfg.Connection.BaseConnInfo.Model,
		"messages":          messagePayload,
		"last_message":      lastMessage,
		"last_user_message": lastUserMessage,
		"system_message":    strings.Join(systemMessages, "\n"),
		"all_messages_text": strings.Join(allMessagesText, "\n"),
		"api_key":           r.cfg.Connection.BaseConnInfo.APIKey,
	}

	if r.params != nil {
		if r.params.Temperature != nil {
			vars["temperature"] = *r.params.Temperature
		}
		if r.params.MaxTokens != 0 {
			vars["max_tokens"] = r.params.MaxTokens
		}
		if r.params.TopP != nil && *r.params.TopP != 0 {
			vars["top_p"] = *r.params.TopP
		}
		if r.params.FrequencyPenalty != 0 {
			vars["frequency_penalty"] = r.params.FrequencyPenalty
		}
		if r.params.PresencePenalty != 0 {
			vars["presence_penalty"] = r.params.PresencePenalty
		}
	}

	for target, source := range r.inputMapping {
		vars[target] = resolveTemplateValue(source, vars)
	}

	return vars
}

func sampleProbeMessages() []*schema.Message {
	return []*schema.Message{
		schema.SystemMessage("You are a helpful assistant."),
		schema.UserMessage("1+1=?"),
	}
}

func joinCustomHTTPURL(baseURL, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		if strings.TrimSpace(baseURL) == "" {
			return "", errors.New("custom HTTP base URL is empty")
		}
		return baseURL, nil
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path, nil
	}
	if strings.TrimSpace(baseURL) == "" {
		return "", errors.New("custom HTTP base URL is empty")
	}
	return url.JoinPath(baseURL, path)
}

func renderJSONTemplate(template string, vars map[string]any) (string, error) {
	rendered := customHTTPTemplatePattern.ReplaceAllStringFunc(template, func(token string) string {
		name := extractTemplateName(token)
		value, ok := vars[name]
		if !ok {
			return "null"
		}
		buf, err := json.Marshal(value)
		if err != nil {
			return "null"
		}
		return string(buf)
	})

	var js any
	if err := json.Unmarshal([]byte(rendered), &js); err != nil {
		return "", fmt.Errorf("invalid payload template after render: %w", err)
	}

	return rendered, nil
}

func renderStringTemplate(template string, vars map[string]any) string {
	return customHTTPTemplatePattern.ReplaceAllStringFunc(template, func(token string) string {
		name := extractTemplateName(token)
		value, ok := vars[name]
		if !ok {
			return ""
		}
		return stringifyJSONValue(value)
	})
}

func extractTemplateName(token string) string {
	token = strings.TrimSpace(token)
	token = strings.TrimPrefix(token, "{{")
	token = strings.TrimSuffix(token, "}}")
	return strings.TrimSpace(token)
}

func resolveTemplateValue(source string, vars map[string]any) any {
	source = strings.TrimSpace(source)
	if source == "" {
		return ""
	}
	if strings.HasPrefix(source, "{{") && strings.HasSuffix(source, "}}") {
		source = extractTemplateName(source)
	}
	if value, ok := vars[source]; ok {
		return value
	}
	return source
}

func jsonLookup(data any, path string) (any, bool) {
	if path == "" {
		return data, true
	}

	current := data
	segments := strings.Split(path, ".")
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		switch typed := current.(type) {
		case map[string]any:
			next, ok := typed[segment]
			if !ok {
				return nil, false
			}
			current = next
		case []any:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(typed) {
				return nil, false
			}
			current = typed[index]
		default:
			return nil, false
		}
	}

	return current, true
}

func stringifyJSONValue(v any) string {
	switch typed := v.(type) {
	case nil:
		return ""
	case string:
		return typed
	case json.Number:
		return typed.String()
	case bool:
		return strconv.FormatBool(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 32)
	case int:
		return strconv.Itoa(typed)
	case int32:
		return strconv.FormatInt(int64(typed), 10)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		buf, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(buf)
	}
}

func isEmptyJSONValue(v any) bool {
	switch typed := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return false
	}
}
