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

package openaiproxy

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/coze-dev/coze-studio/backend/pkg/envkey"
)

const (
	EnvKey         = "OPENAI_PROXY"
	defaultTimeout = 60 * time.Second
)

func NewHTTPClientFromEnv() (*http.Client, error) {
	proxyAddr := envkey.GetString(EnvKey)
	if proxyAddr == "" {
		return nil, nil
	}

	proxyURL, err := url.Parse(proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", EnvKey, err)
	}

	return &http.Client{
		Timeout:   defaultTimeout,
		Transport: newProxyTransport(proxyURL),
	}, nil
}

func newProxyTransport(proxyURL *url.URL) *http.Transport {
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	}

	transport := defaultTransport.Clone()
	transport.Proxy = http.ProxyURL(proxyURL)

	return transport
}
