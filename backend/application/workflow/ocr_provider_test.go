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
	"encoding/json"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"

	domainWorkflow "github.com/coze-dev/coze-studio/backend/domain/workflow"
	"github.com/coze-dev/coze-studio/backend/domain/workflow/config"
	mockWorkflow "github.com/coze-dev/coze-studio/backend/internal/mock/domain/workflow"
)

func TestListOCRProvidersReturnsSafeFieldsOnly(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := mockWorkflow.NewMockRepository(ctrl)
	domainWorkflow.SetRepository(repo)
	defer domainWorkflow.SetRepository(nil)

	repo.EXPECT().GetOCRProviders().Return([]*config.OCRProvider{
		{
			ID:       "deepseek_ocr2_gpu7",
			Name:     "DeepSeek OCR 2",
			Enabled:  true,
			Endpoint: "http://10.0.0.7:17007",
			APIKey:   "secret",
			Model:    "deepseek-ai/DeepSeek-OCR-2",
			Capabilities: &config.OCRProviderCapability{
				InputTypes: []string{"image"},
			},
		},
	})

	resp := (&ApplicationService{}).ListOCRProviders(context.Background())
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	body := string(raw)
	for _, leak := range []string{"10.0.0.7", "secret", "DeepSeek-OCR-2"} {
		if strings.Contains(body, leak) {
			t.Fatalf("safe provider response leaked %q: %s", leak, body)
		}
	}
	if !strings.Contains(body, "deepseek_ocr2_gpu7") {
		t.Fatalf("safe provider response missing provider id: %s", body)
	}
}
