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
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/coze-dev/coze-studio/backend/pkg/ctxcache"
)

type customHTTPTemplateVarsCacheKey struct{}

type CustomHTTPSchemaField struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	Desc     string `json:"desc"`
}

var supportedCustomHTTPSchemaTypes = map[string]struct{}{
	"string":  {},
	"number":  {},
	"integer": {},
	"boolean": {},
	"object":  {},
	"array":   {},
}

func ParseCustomHTTPSchemaJSON(raw string) ([]CustomHTTPSchemaField, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("schema is required")
	}

	var fields []CustomHTTPSchemaField
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return nil, fmt.Errorf("parse schema json failed: %w", err)
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("schema must define at least one field")
	}

	seen := make(map[string]struct{}, len(fields))
	for i, field := range fields {
		fieldName := strings.TrimSpace(field.Name)
		if fieldName == "" {
			return nil, fmt.Errorf("schema field %d name is required", i)
		}
		fieldType := strings.ToLower(strings.TrimSpace(field.Type))
		if _, ok := supportedCustomHTTPSchemaTypes[fieldType]; !ok {
			return nil, fmt.Errorf("schema field %q has unsupported type %q", fieldName, field.Type)
		}
		if _, exists := seen[fieldName]; exists {
			return nil, fmt.Errorf("schema field %q is duplicated", fieldName)
		}
		seen[fieldName] = struct{}{}
		fields[i].Name = fieldName
		fields[i].Type = fieldType
		fields[i].Label = strings.TrimSpace(field.Label)
		fields[i].Desc = strings.TrimSpace(field.Desc)
	}

	return fields, nil
}

func StoreCustomHTTPTemplateVars(ctx context.Context, vars map[string]any) {
	if len(vars) == 0 {
		return
	}

	cloned := make(map[string]any, len(vars))
	for k, v := range vars {
		cloned[k] = v
	}
	ctxcache.Store(ctx, customHTTPTemplateVarsCacheKey{}, cloned)
}

func LoadCustomHTTPTemplateVars(ctx context.Context) map[string]any {
	vars, ok := ctxcache.Get[map[string]any](ctx, customHTTPTemplateVarsCacheKey{})
	if !ok {
		return nil
	}

	return vars
}
