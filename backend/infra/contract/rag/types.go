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

// ModelProvider mirrors rag's ModelProviderDTO. The wire fields kept here are
// the ones coze actually consumes; rag may serialise additional fields and Go's
// JSON decoder silently ignores them.
type ModelProvider struct {
	ModelID      string    `json:"model_id"`
	Type         string    `json:"type"` // "text" | "image"
	Name         string    `json:"name"`
	ModelName    string    `json:"model_name"`
	Dimensions   *int      `json:"dimensions,omitempty"`
	Capabilities []string  `json:"capabilities,omitempty"`
	Modalities   []string  `json:"modalities,omitempty"`
	Provider     string    `json:"provider,omitempty"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    RagTime `json:"created_at"`
	UpdatedAt    RagTime `json:"updated_at"`
}

// ListModelProvidersResponse mirrors rag's ModelProviderListResponse — a single
// flat list with a discriminating Type field. Callers wanting a split view
// (text vs image) should call Split.
type ListModelProvidersResponse struct {
	Items []ModelProvider `json:"items"`
}

// Split partitions the flat list by Type. Items with an unknown type are
// dropped; that is intentional — surfacing an unrecognised provider to the UI
// would just confuse a user who can't pick it anyway.
func (r *ListModelProvidersResponse) Split() (textModels, imageModels []ModelProvider) {
	if r == nil {
		return nil, nil
	}
	for _, m := range r.Items {
		switch m.Type {
		case "text":
			textModels = append(textModels, m)
		case "image":
			imageModels = append(imageModels, m)
		}
	}
	return textModels, imageModels
}

type FusionPolicy struct {
	Mode    string             `json:"mode"`
	RrfK    int                `json:"rrf_k"`
	Weights map[string]float64 `json:"weights,omitempty"`
}

// CreateKBRequest is the JSON body sent on POST /api/v1/knowledgebases. The
// tenant is carried by the X-Tenant-Id header and is intentionally NOT a
// field here — putting it in the body would be silently ignored by rag and
// mask a misconfigured header.
type CreateKBRequest struct {
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

// KB is the trimmed view of rag's KnowledgeBaseDetail that coze persists
// downstream. Rag returns many additional fields (default chunk size,
// supported modes, etc.) which Go silently drops on unmarshal — DO NOT add
// fields here unless coze actually needs them: every field is a contract
// surface we have to keep aligned.
type KB struct {
	KBID                  string    `json:"kb_id"`
	Name                  string    `json:"name"`
	Description           string    `json:"description"`
	TextEmbeddingModelID  string    `json:"text_embedding_model_id"`
	ImageEmbeddingModelID string    `json:"image_embedding_model_id"`
	Status                string    `json:"status"`
	CreatedAt             RagTime `json:"created_at"`
	UpdatedAt             RagTime `json:"updated_at"`
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

// CreateDocumentRequest is the in-memory representation of the multipart body
// for POST /api/v1/knowledgebases/{kb_id}/documents. The JSON tags are
// intentionally absent — this struct is NEVER marshalled; the Client builds a
// multipart/form-data body field-by-field. The tenant comes from the
// X-Tenant-Id header, not the form. See rag's app/api/routes/documents.py
// upload_document for the authoritative contract.
type CreateDocumentRequest struct {
	// Required: the file bytes (loaded into memory; storage is []byte-based).
	FileBytes []byte
	// Required: file's display name; becomes the multipart filename attribute.
	Filename string
	// Required: rag's file_type form field (e.g. "pdf", "txt", "docx").
	FileType string
	// Required: rag's source_modality enum — text_source | image_source | scanned_document_source.
	SourceModality string
	// Optional: rag's chunk_size form field; nil means "rag's default".
	ChunkSize *int
	// Optional: rag's chunk_overlap form field; nil means "rag's default".
	ChunkOverlap *int
	// Optional: rag's extra_metadata form field. JSON-stringified by the
	// caller; empty string means "omit the field".
	ExtraMetadata string
}

type CreateDocumentResponse struct {
	DocID  string `json:"doc_id"`
	TaskID string `json:"task_id"`
	Status string `json:"status"` // pending | processing | ready | failed
}

// Document mirrors rag's DocumentDetail as of 0e1f49b. The wire shape changed
// in the 2026-05-14 round-2 audit: KBID was dropped (the kb_id lives in the
// URL path), Name was renamed to Filename, and FileType / ChunkCount /
// ErrorMsg / SourceModality are new. Rag also emits delete_cleanup_errors,
// processing_config, processing_summary at the top level — coze ignores those
// here; adding fields means adding contract surface we have to maintain.
type Document struct {
	DocID          string  `json:"doc_id"`
	Filename       string  `json:"filename"`
	FileType       string  `json:"file_type"`
	Status         string  `json:"status"`
	ChunkCount     int     `json:"chunk_count"`
	ErrorMsg       string  `json:"error_msg,omitempty"`
	SourceModality string  `json:"source_modality"`
	CreatedAt      RagTime `json:"created_at"`
	UpdatedAt      RagTime `json:"updated_at"`
}

type ListDocumentsResponse struct {
	Items []Document `json:"items"`
	Total int        `json:"total"`
}

// Task mirrors rag's TaskDetail as of 0e1f49b. The wire shape changed in the
// 2026-05-14 round-2 audit: DocID and Progress were dropped; Error was renamed
// to ErrorMsg; UpdatedAt became FinishedAt; CreatedAt/StartedAt/Type/RetryCount
// are new. Pre-transition phases emit JSON null for StartedAt/FinishedAt, which
// is why they're pointer-typed — a value receiver would decode null into the
// unix epoch, masking the unset state.
type Task struct {
	TaskID     string   `json:"task_id"`
	Type       string   `json:"type"` // "ingestion" today; future types may exist
	Status     string   `json:"status"` // pending | running | retrying | success | failed
	RetryCount int      `json:"retry_count"`
	ErrorMsg   string   `json:"error_msg,omitempty"`
	CreatedAt  RagTime  `json:"created_at"`
	StartedAt  *RagTime `json:"started_at,omitempty"`
	FinishedAt *RagTime `json:"finished_at,omitempty"`
}

// RetrieveRequest mirrors rag's RetrievalRequest. Tenant comes from the
// X-Tenant-Id header, not the body. Doc-level filtering is intentionally not
// exposed as a top-level field — rag's /retrieval endpoint has no `doc_ids`
// parameter; callers that need it must place it under Filters and rely on
// rag's metadata-filter behaviour (see rag/docs §10.4).
type RetrieveRequest struct {
	KBIDs            []string       `json:"kb_ids"`
	Query            *string        `json:"query,omitempty"`
	QueryImage       *string        `json:"query_image,omitempty"`
	QueryMode        string         `json:"query_mode,omitempty"`
	SearchType       string         `json:"search_type,omitempty"`
	TopK             *int           `json:"top_k,omitempty"`
	CandidateK       *int           `json:"candidate_k,omitempty"`
	Filters          map[string]any `json:"filters,omitempty"`
	TargetChunkTypes []string       `json:"target_chunk_types,omitempty"`
	Retrievers       []string       `json:"retrievers,omitempty"`
	FusionPolicy     map[string]any `json:"fusion_policy,omitempty"`
	RetrieverParams  map[string]any `json:"retriever_params,omitempty"`
	QueryStrategy    map[string]any `json:"query_strategy,omitempty"`
}

// RetrieveHit mirrors RetrievalHitDTO. Only the fields coze consumes are
// declared; rag may return more (modality_payload, hit_modalities, etc.)
// and Go will drop them silently on unmarshal.
type RetrieveHit struct {
	ChunkID  string         `json:"chunk_id"`
	KBID     string         `json:"kb_id"`
	DocID    string         `json:"doc_id"`
	DocName  string         `json:"doc_name,omitempty"`
	Score    float64        `json:"score"`
	Content  string         `json:"content,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type RetrieveResponse struct {
	Items []RetrieveHit  `json:"items"`
	Debug map[string]any `json:"debug,omitempty"`
}

