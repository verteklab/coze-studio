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

package rag

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_OK(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "rag.yaml")
	body := `rag:
  base_url: "http://x:8000"
  timeout_ms: 5000
  upload_timeout_ms: 30000
  retrieval_timeout_ms: 10000
  max_retries: 1
  retry_backoff_ms: 100
  default_text_embedding_model_id: "t"
  default_image_embedding_model_id: "i"
knowledge:
  backend: "rag"
  tenant:
    mode: "env"
    default_tenant_id: "coze"
`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Rag.BaseURL != "http://x:8000" {
		t.Fatalf("base_url=%s", c.Rag.BaseURL)
	}
	if c.Knowledge.Backend != "rag" {
		t.Fatalf("backend=%s", c.Knowledge.Backend)
	}
	if c.Knowledge.Tenant.Mode != "env" {
		t.Fatalf("tenant.mode=%s", c.Knowledge.Tenant.Mode)
	}
	if c.Knowledge.Tenant.DefaultTenantID != "coze" {
		t.Fatalf("tenant.default=%s", c.Knowledge.Tenant.DefaultTenantID)
	}
	if c.Rag.Timeout.Milliseconds() != 5000 {
		t.Fatalf("timeout=%v", c.Rag.Timeout)
	}
}

func TestLoad_EnvSubstitution(t *testing.T) {
	t.Setenv("MY_BASE", "http://envset:9000")
	dir := t.TempDir()
	p := filepath.Join(dir, "rag.yaml")
	body := `rag:
  base_url: "${MY_BASE}"
  timeout_ms: 1000
  upload_timeout_ms: 1000
  retrieval_timeout_ms: 1000
  max_retries: 0
  retry_backoff_ms: 0
  default_text_embedding_model_id: ""
  default_image_embedding_model_id: ""
knowledge:
  backend: "legacy"
  tenant:
    mode: "env"
    default_tenant_id: "coze"
`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Rag.BaseURL != "http://envset:9000" {
		t.Fatalf("base_url=%s", c.Rag.BaseURL)
	}
}

func TestLoad_DefaultSubstitution(t *testing.T) {
	// Ensure the env var is unset so the default fires.
	if err := os.Unsetenv("RAG_BASE_URL_TEST_DEFAULT"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "rag.yaml")
	body := `rag:
  base_url: "${RAG_BASE_URL_TEST_DEFAULT:http://default:1234}"
  timeout_ms: 1
  upload_timeout_ms: 1
  retrieval_timeout_ms: 1
  max_retries: 0
  retry_backoff_ms: 0
  default_text_embedding_model_id: ""
  default_image_embedding_model_id: ""
knowledge:
  backend: "${KNOWLEDGE_BACKEND_TEST_DEFAULT:legacy}"
  tenant:
    mode: "${RAG_TENANT_MODE_TEST_DEFAULT:env}"
    default_tenant_id: "${RAG_TENANT_ID_TEST_DEFAULT:coze}"
`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Rag.BaseURL != "http://default:1234" {
		t.Fatalf("base_url=%s", c.Rag.BaseURL)
	}
	if c.Knowledge.Backend != "legacy" {
		t.Fatalf("backend=%s", c.Knowledge.Backend)
	}
	if c.Knowledge.Tenant.Mode != "env" {
		t.Fatalf("tenant.mode=%s", c.Knowledge.Tenant.Mode)
	}
	if c.Knowledge.Tenant.DefaultTenantID != "coze" {
		t.Fatalf("tenant.default=%s", c.Knowledge.Tenant.DefaultTenantID)
	}
}
