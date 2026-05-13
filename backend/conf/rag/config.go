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

// Package rag holds the configuration loader for the rag service integration.
//
// The yaml document supports environment variable substitution in the form
// ${VAR} and ${VAR:default}. Go's stdlib os.ExpandEnv only recognises ${VAR}
// and $VAR, so we expand defaults ourselves; otherwise the literal ":default"
// would survive into the parsed value.
package rag

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds the runtime knobs for talking to the rag service.
type Config struct {
	BaseURL                      string        `yaml:"base_url"`
	Timeout                      time.Duration `yaml:"-"`
	TimeoutMs                    int           `yaml:"timeout_ms"`
	UploadTimeoutMs              int           `yaml:"upload_timeout_ms"`
	RetrievalTimeoutMs           int           `yaml:"retrieval_timeout_ms"`
	MaxRetries                   int           `yaml:"max_retries"`
	RetryBackoffMs               int           `yaml:"retry_backoff_ms"`
	DefaultTextEmbeddingModelID  string        `yaml:"default_text_embedding_model_id"`
	DefaultImageEmbeddingModelID string        `yaml:"default_image_embedding_model_id"`
}

// FileConfig is the on-disk shape of backend/conf/rag/rag.yaml.
type FileConfig struct {
	Rag       Config           `yaml:"rag"`
	Knowledge KnowledgeBackend `yaml:"knowledge"`
}

// KnowledgeBackend selects which knowledge backend the application uses and
// how tenants are scoped against the rag service.
type KnowledgeBackend struct {
	Backend string       `yaml:"backend"`
	Tenant  TenantConfig `yaml:"tenant"`
}

// TenantConfig controls the tenant scoping strategy.
//
//	mode=env  -> use DefaultTenantID for every request (Phase 1)
//	mode=user -> derive the tenant from the authenticated user (Phase 2)
type TenantConfig struct {
	Mode            string `yaml:"mode"`
	DefaultTenantID string `yaml:"default_tenant_id"`
}

// envVarRe matches ${VAR} or ${VAR:default}. The variable name is restricted
// to the conventional [A-Z0-9_] alphabet; the default value greedily matches
// anything but '}' so URLs and paths with colons (e.g. http://) are fine.
var envVarRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::([^}]*))?\}`)

// expandEnv replaces ${VAR} and ${VAR:default} occurrences in s with the
// corresponding environment values. Unset (or empty) variables with no default
// expand to the empty string, matching docker-compose semantics.
func expandEnv(s string) string {
	return envVarRe.ReplaceAllStringFunc(s, func(match string) string {
		m := envVarRe.FindStringSubmatch(match)
		name, def := m[1], m[2]
		if v, ok := os.LookupEnv(name); ok && v != "" {
			return v
		}
		return def
	})
}

// Load reads, expands, and parses the rag config file at path.
func Load(path string) (*FileConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read rag config: %w", err)
	}
	expanded := expandEnv(string(b))
	var c FileConfig
	if err := yaml.Unmarshal([]byte(expanded), &c); err != nil {
		return nil, fmt.Errorf("parse rag config: %w", err)
	}
	c.Rag.Timeout = time.Duration(c.Rag.TimeoutMs) * time.Millisecond
	// Defensive trim: ${VAR:default} can leave trailing whitespace if the
	// yaml is hand-edited and a value sits flush against the closing brace.
	c.Rag.BaseURL = strings.TrimSpace(c.Rag.BaseURL)
	c.Knowledge.Backend = strings.TrimSpace(c.Knowledge.Backend)
	c.Knowledge.Tenant.Mode = strings.TrimSpace(c.Knowledge.Tenant.Mode)
	c.Knowledge.Tenant.DefaultTenantID = strings.TrimSpace(c.Knowledge.Tenant.DefaultTenantID)
	return &c, nil
}
