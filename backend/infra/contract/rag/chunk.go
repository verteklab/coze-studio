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

// Chunk DTOs for rag's manual chunk CRUD endpoints (see
// rag/app/api/routes/chunks.py + rag/app/api/schemas/chunk.py as of
// 2026-05-15). Field names mirror rag's pydantic models exactly; coze's
// JSON decoder silently ignores fields not declared here (forward-compatible
// against rag adding new fields), and `omitempty` is used on the request
// side so absent fields don't override rag defaults.
//
// `kb_id` / `doc_id` / `chunk_id` are all string UUIDs on the rag side; the
// int64 ↔ string translation lives in ragimpl, NOT here.

// ChunkImage carries the image-chunk-specific fields. text_chunk responses
// omit the parent `image` object entirely; image_chunk requests must include
// at least `image_ref`.
type ChunkImage struct {
	ImageRef string `json:"image_ref,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	OCRText  string `json:"ocr_text,omitempty"`
	OCRUsed  bool   `json:"ocr_used,omitempty"`
	Caption  string `json:"caption,omitempty"`
}

// ChunkPosition carries position hints for CreateChunk. sequence_index is
// optional; omitting it appends to the end of the document. When present,
// rag bumps the sequence of every existing chunk >= sequence_index by one.
type ChunkPosition struct {
	SequenceIndex *int `json:"sequence_index,omitempty"`
}

// CreateChunkRequest is the body for `POST .../documents/{doc_id}/chunks`.
// chunk_type is required ("text_chunk" or "image_chunk"); other fields are
// chunk-type-dependent.
type CreateChunkRequest struct {
	ChunkType string         `json:"chunk_type"`
	Content   string         `json:"content,omitempty"`
	Image     *ChunkImage    `json:"image,omitempty"`
	Position  *ChunkPosition `json:"position,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// UpdateChunkRequest is the body for
// `POST .../documents/{doc_id}/chunks/{chunk_id}/update`. Every field is a
// pointer / map / object so that rag's exclude_unset can distinguish "omit"
// from "set to empty". Content must be non-empty when present (rag enforces
// via pydantic validator).
type UpdateChunkRequest struct {
	Content  *string        `json:"content,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Image    *ChunkImage    `json:"image,omitempty"`
}

// Chunk mirrors rag's ChunkDetail (response shape for all single-chunk
// endpoints + list items). Optional fields use pointers or zero-values
// matching rag's pydantic defaults (Optional[...] → pointer-typed when
// "null vs absent" matters; default_factory → zero-valued map/struct).
//
// Fields preserved here but not currently consumed by coze (e.g. `source`,
// `position`, `source_modality`, `source_ref`, `modality_payload`) are kept
// so future ragimpl callers don't have to extend this struct under load.
// Each is a contract surface: only add fields coze actually needs.
type Chunk struct {
	ChunkID         string         `json:"chunk_id"`
	DocID           string         `json:"doc_id"`
	KBID            string         `json:"kb_id"`
	DocName         string         `json:"doc_name,omitempty"`
	ChunkType       string         `json:"chunk_type"`
	SequenceIndex   *int           `json:"sequence_index,omitempty"`
	Content         string         `json:"content,omitempty"`
	Image           *ChunkImage    `json:"image,omitempty"`
	CharCount       int            `json:"char_count"`
	ByteCount       int            `json:"byte_count"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	Source          map[string]any `json:"source,omitempty"`
	Position        map[string]any `json:"position,omitempty"`
	SourceModality  string         `json:"source_modality,omitempty"`
	SourceRef       string         `json:"source_ref,omitempty"`
	ModalityPayload map[string]any `json:"modality_payload,omitempty"`
	Status          string         `json:"status,omitempty"`
	CreatedAt       string         `json:"created_at,omitempty"`
	UpdatedAt       string         `json:"updated_at,omitempty"`
}

// ListChunksQuery scopes a `GET .../documents/{doc_id}/chunks` request.
// Page is 1-indexed; PageSize must be in [1, 200] per rag's validator.
type ListChunksQuery struct {
	Page          int
	PageSize      int
	Keyword       string
	ChunkType     string
	AfterSequence *int
}

// ListChunksByKBQuery scopes a KB-level `GET .../chunks` request. Same
// shape as ListChunksQuery plus an optional DocIDs filter. has_caption is
// intentionally absent -- rag has not implemented that filter, so coze
// handles it client-side; see ragimpl.ListPhotoSlice.
type ListChunksByKBQuery struct {
	Page          int
	PageSize      int
	Keyword       string
	ChunkType     string
	DocIDs        []string
	AfterSequence *int
}

// ListChunksResponse is the response data for both list endpoints.
type ListChunksResponse struct {
	Items    []Chunk `json:"items"`
	Total    int     `json:"total"`
	Page     int     `json:"page"`
	PageSize int     `json:"page_size"`
}

// MGetChunksRequest mirrors rag's MGetChunksRequest body shape.
type MGetChunksRequest struct {
	ChunkIDs []string `json:"chunk_ids"`
}

// MGetChunksResponse holds the response data for `POST .../chunks:mget`.
// Items preserve request order; deleted chunks appear in-place as
// {chunk_id, deleted: true} placeholders. We keep the field as []Chunk
// because the deleted-placeholder fields (chunk_id + deleted) decode
// cleanly into Chunk with most fields zero; consumers should branch on
// `Deleted` to distinguish.
type MGetChunksResponse struct {
	Items []MGetChunksItem `json:"items"`
}

// MGetChunksItem is one entry in MGetChunksResponse.Items. When Deleted is
// true the remaining fields are zero-valued except ChunkID; callers must
// branch on Deleted before reading content/metadata.
type MGetChunksItem struct {
	Chunk
	Deleted bool `json:"deleted,omitempty"`
}

// DeleteChunkResponse mirrors `POST .../chunks/{chunk_id}/delete`. Body is
// `{"deleted": true}` on success; we surface the bool so an integration
// test can lock it, but most callers only need the lack of an error.
type DeleteChunkResponse struct {
	Deleted bool `json:"deleted"`
}
