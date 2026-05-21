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

package workflow

import (
	"context"

	domainWorkflow "github.com/coze-dev/coze-studio/backend/domain/workflow"
	"github.com/coze-dev/coze-studio/backend/domain/workflow/config"
)

type OCRProviderListResponse struct {
	Code int32              `json:"code"`
	Msg  string             `json:"msg"`
	Data []*SafeOCRProvider `json:"data"`
}

type SafeOCRProvider struct {
	ID           string                        `json:"id"`
	Name         string                        `json:"name"`
	Description  string                        `json:"description,omitempty"`
	Enabled      bool                          `json:"enabled"`
	Capabilities *config.OCRProviderCapability `json:"capabilities,omitempty"`
}

func (w *ApplicationService) ListOCRProviders(_ context.Context) *OCRProviderListResponse {
	providers := domainWorkflow.GetRepository().GetOCRProviders()
	safeProviders := make([]*SafeOCRProvider, 0, len(providers))
	for _, provider := range providers {
		if provider == nil || !provider.Enabled {
			continue
		}
		safeProviders = append(safeProviders, &SafeOCRProvider{
			ID:           provider.ID,
			Name:         provider.Name,
			Description:  provider.Description,
			Enabled:      provider.Enabled,
			Capabilities: provider.Capabilities,
		})
	}
	return &OCRProviderListResponse{
		Code: 0,
		Msg:  "",
		Data: safeProviders,
	}
}
