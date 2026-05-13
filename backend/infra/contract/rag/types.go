/*
 * Copyright 2026 coze-dev Authors
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

import "time"

type ModelProvider struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"` // "text" | "image"
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Dim      int    `json:"dim"`
}

type ListModelProvidersResponse struct {
	TextModels  []ModelProvider `json:"text_models"`
	ImageModels []ModelProvider `json:"image_models"`
}

type FusionPolicy struct {
	Mode    string             `json:"mode"`
	RrfK    int                `json:"rrf_k"`
	Weights map[string]float64 `json:"weights"`
}

type CreateKBRequest struct {
	TenantID                  string         `json:"tenant_id"`
	Name                      string         `json:"name"`
	Description               string         `json:"description,omitempty"`
	TextEmbeddingModelID      string         `json:"text_embedding_model_id"`
	ImageEmbeddingModelID     string         `json:"image_embedding_model_id"`
	EnabledChunkTypes         []string       `json:"enabled_chunk_types"`
	SupportedSourceModalities []string       `json:"supported_source_modalities"`
	EnabledRetrievers         []string       `json:"enabled_retrievers,omitempty"`
	SupportedQueryModes       []string       `json:"supported_query_modes,omitempty"`
	SupportedSearchTypes      []string       `json:"supported_search_types,omitempty"`
	MetadataSchema            map[string]any `json:"metadata_schema,omitempty"`
	DefaultFusionPolicy       FusionPolicy   `json:"default_fusion_policy"`
}

type KB struct {
	KBID                  string    `json:"kb_id"`
	Name                  string    `json:"name"`
	Description           string    `json:"description"`
	TextEmbeddingModelID  string    `json:"text_embedding_model_id"`
	ImageEmbeddingModelID string    `json:"image_embedding_model_id"`
	Status                string    `json:"status"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type UpdateKBRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Status      *string `json:"status,omitempty"`
}

type ListKBsRequest struct {
	TenantID string
	Page     int
	PageSize int
}

type ListKBsResponse struct {
	Items []KB `json:"items"`
	Total int  `json:"total"`
}

type CreateDocumentRequest struct {
	TenantID         string         `json:"tenant_id"`
	SourceURI        string         `json:"source_uri"`
	SourceModality   string         `json:"source_modality"` // text_source | image_source | scanned_document_source
	ParsingStrategy  map[string]any `json:"parsing_strategy,omitempty"`
	ChunkingStrategy map[string]any `json:"chunking_strategy,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
}

type CreateDocumentResponse struct {
	DocID  string `json:"doc_id"`
	TaskID string `json:"task_id"`
	Status string `json:"status"` // pending | processing | ready | failed
}

type Document struct {
	DocID     string    `json:"doc_id"`
	KBID      string    `json:"kb_id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ListDocumentsResponse struct {
	Items []Document `json:"items"`
	Total int        `json:"total"`
}

type Task struct {
	TaskID    string    `json:"task_id"`
	DocID     string    `json:"doc_id"`
	Status    string    `json:"status"` // pending | running | retrying | success | failed
	Progress  int       `json:"progress"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type RetrieveRequest struct {
	TenantID   string         `json:"tenant_id"`
	KBIDs      []string       `json:"kb_ids"`
	DocIDs     []string       `json:"doc_ids,omitempty"`
	Query      string         `json:"query"`
	QueryMode  string         `json:"query_mode"` // text_input | image_input | mixed_input
	TopK       int            `json:"top_k,omitempty"`
	MinScore   float64        `json:"min_score,omitempty"`
	MaxTokens  int            `json:"max_tokens,omitempty"`
	SearchType string         `json:"search_type,omitempty"` // semantic | fulltext | hybrid
	QueryStrat map[string]any `json:"query_strategy,omitempty"`
	Rerank     map[string]any `json:"rerank,omitempty"`
}

type RetrieveHit struct {
	ChunkID  string         `json:"chunk_id"`
	DocID    string         `json:"doc_id"`
	Score    float64        `json:"score"`
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type RetrieveResponse struct {
	Hits  []RetrieveHit  `json:"hits"`
	Debug map[string]any `json:"debug,omitempty"`
}

type ErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
