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
	"fmt"
	"strings"
)

const defaultOCRProviderID = "paddle-ocr"

var legacyOCRProviderIDs = map[string]string{
	"deepseek_ocr2_gpu7":       defaultOCRProviderID,
	"paddleocr-openai-pdf-api": defaultOCRProviderID,
}

type WorkflowConfig struct {
	NodeOfCodeConfig *NodeOfCodeConfig `yaml:"NodeOfCodeConfig"`
	OCRProviders     []*OCRProvider    `yaml:"ocr_providers"`
}

func (w *WorkflowConfig) GetNodeOfCodeConfig() *NodeOfCodeConfig {
	return w.NodeOfCodeConfig
}

func (w *WorkflowConfig) GetOCRProviders() []*OCRProvider {
	return w.OCRProviders
}

func (w *WorkflowConfig) GetOCRProviderByID(id string) (*OCRProvider, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("ocr provider id is required")
	}
	if mappedID, ok := legacyOCRProviderIDs[id]; ok {
		id = mappedID
	}
	for _, provider := range w.OCRProviders {
		if provider == nil || !provider.Enabled {
			continue
		}
		if provider.ID == id {
			return provider, nil
		}
	}
	return nil, fmt.Errorf("ocr provider %q not found or disabled", id)
}

type NodeOfCodeConfig struct {
	SupportThirdPartModules []string `yaml:"SupportThirdPartModules"`
}

func (n *NodeOfCodeConfig) GetSupportThirdPartModules() []string {
	return n.SupportThirdPartModules
}

type OCRProvider struct {
	ID           string                 `yaml:"id" json:"id"`
	Name         string                 `yaml:"name" json:"name"`
	Description  string                 `yaml:"description,omitempty" json:"description,omitempty"`
	Enabled      bool                   `yaml:"enabled" json:"enabled"`
	Format       string                 `yaml:"format" json:"format"`
	Endpoint     string                 `yaml:"endpoint" json:"-"`
	APIKey       string                 `yaml:"api_key,omitempty" json:"-"`
	Model        string                 `yaml:"model" json:"-"`
	AllowedHosts []string               `yaml:"allowed_hosts,omitempty" json:"-"`
	ResponsePath string                 `yaml:"response_path,omitempty" json:"-"`
	Capabilities *OCRProviderCapability `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
}

type OCRProviderCapability struct {
	InputTypes        []string `yaml:"input_types,omitempty" json:"input_types,omitempty"`
	SupportsPDF       bool     `yaml:"supports_pdf" json:"supports_pdf"`
	SupportsPageRange bool     `yaml:"supports_page_range" json:"supports_page_range"`
}
