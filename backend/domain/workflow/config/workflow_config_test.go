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

package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestWorkflowConfigOCRProvidersYAMLAndResolver(t *testing.T) {
	raw := []byte(`
ocr_providers:
  - id: paddle-ocr
    name: paddle-ocr
    enabled: true
    format: paddleocr
    endpoint: http://10.0.0.7:17006
    api_key: secret
    model: paddleocr-pdf
    allowed_hosts:
      - 10.0.0.7
    response_path: choices.0.message.content
    capabilities:
      input_types: [image, pdf]
      supports_pdf: true
      supports_page_range: false
`)

	var cfg WorkflowConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal workflow config: %v", err)
	}

	providers := cfg.GetOCRProviders()
	if len(providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(providers))
	}
	if providers[0].Endpoint == "" || providers[0].Model == "" || providers[0].APIKey == "" {
		t.Fatal("expected full provider config to keep execution-only fields")
	}

	provider, err := cfg.GetOCRProviderByID("paddle-ocr")
	if err != nil {
		t.Fatalf("get provider: %v", err)
	}
	if provider.ResponsePath != "choices.0.message.content" {
		t.Fatalf("unexpected response path: %s", provider.ResponsePath)
	}

	if _, err := cfg.GetOCRProviderByID("missing"); err == nil {
		t.Fatal("expected missing provider error")
	}
}

func TestWorkflowConfigOCRProviderLegacyAlias(t *testing.T) {
	cfg := WorkflowConfig{
		OCRProviders: []*OCRProvider{
			{
				ID:      "paddle-ocr",
				Name:    "paddle-ocr",
				Enabled: true,
			},
		},
	}

	provider, err := cfg.GetOCRProviderByID("deepseek_ocr2_gpu7")
	if err != nil {
		t.Fatalf("legacy provider alias should resolve: %v", err)
	}
	if provider.ID != "paddle-ocr" {
		t.Fatalf("legacy provider alias resolved to %q", provider.ID)
	}
}
