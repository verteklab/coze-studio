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

// rag-contract-check fetches rag's /openapi.json and asserts that the
// endpoints coze's Go client expects are still present with the expected
// HTTP method. Run it in CI against a freshly-stood-up rag-web to catch
// breaking contract drift before it lands in production.
//
// Usage:
//
//	rag-contract-check -base http://localhost:8000
//
// Exit codes:
//
//	0  contract holds
//	1  contract violations detected (details on stderr)
//	2  transport / decode failure (rag-web not reachable, malformed JSON)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"
)

type openAPI struct {
	Paths map[string]map[string]any `json:"paths"`
}

// required is the set of (path, method) pairs the coze Go client depends on.
// rag namespaces all business endpoints under /api/v1; only the health probes
// (/health and /ready) sit at the service root and intentionally omit the
// prefix. Document operations are nested under their KB on rag's side —
// flat /documents/{doc_id} routes do NOT exist.
var required = []struct {
	Path   string
	Method string
}{
	{"/ready", "get"},
	{"/api/v1/model-providers", "get"},
	{"/api/v1/knowledgebases", "post"},
	{"/api/v1/knowledgebases", "get"},
	{"/api/v1/knowledgebases/{kb_id}", "get"},
	{"/api/v1/knowledgebases/{kb_id}", "patch"},
	{"/api/v1/knowledgebases/{kb_id}", "delete"},
	{"/api/v1/knowledgebases/{kb_id}/documents", "post"},
	{"/api/v1/knowledgebases/{kb_id}/documents", "get"},
	{"/api/v1/knowledgebases/{kb_id}/documents/{doc_id}", "get"},
	{"/api/v1/knowledgebases/{kb_id}/documents/{doc_id}", "delete"},
	{"/api/v1/tasks/{task_id}", "get"},
	{"/api/v1/retrieval", "post"},
}

func main() {
	base := flag.String("base", "http://localhost:8000", "rag base URL")
	timeout := flag.Duration("timeout", 10*time.Second, "request timeout")
	flag.Parse()

	client := &http.Client{Timeout: *timeout}
	resp, err := client.Get(*base + "/openapi.json")
	if err != nil {
		fmt.Fprintln(os.Stderr, "fetch:", err)
		os.Exit(2)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "fetch: unexpected status %d\n", resp.StatusCode)
		os.Exit(2)
	}

	var oa openAPI
	if err := json.NewDecoder(resp.Body).Decode(&oa); err != nil {
		fmt.Fprintln(os.Stderr, "decode:", err)
		os.Exit(2)
	}

	missing := 0
	for _, r := range required {
		methods, ok := oa.Paths[r.Path]
		if !ok {
			fmt.Fprintf(os.Stderr, "MISSING path: %s\n", r.Path)
			missing++
			continue
		}
		if _, ok := methods[r.Method]; !ok {
			fmt.Fprintf(os.Stderr, "MISSING method %s on path %s\n", r.Method, r.Path)
			missing++
		}
	}
	if missing > 0 {
		fmt.Fprintf(os.Stderr, "%d contract violations\n", missing)
		os.Exit(1)
	}
	fmt.Println("OK")
}
