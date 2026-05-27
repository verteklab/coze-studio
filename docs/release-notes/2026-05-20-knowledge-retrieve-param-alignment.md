# Knowledge-retrieve param alignment (2026-05-20)

The workflow knowledge-retrieve node now sends exactly the request shape
rag accepts. Fields that rag silently dropped (`min_score`, `document_ids`,
`max_tokens`, `is_personal_only`, `use_nl2sql`) have been removed from
the node UI and the request payload. The 4 query-strategy booleans
(`rewrite`, `expansion`, `multi_query`, `enable_rerank`) replace the
former `use_rewrite` / `use_rerank` pair; no `llm_model_id` /
`rerank_model_id` is sent (rag's deployment-level config owns those).

New rag capabilities are now exposed on the node form:
- **filters** (KV editor) — KB metadata-based filter
- **target_chunk_types** — limit retrieval to `text_chunk` or
  `image_chunk`
- **retrievers** — explicit `dense` / `bm25` / `image_vector`
  (advanced; default derived from `target_chunk_types`)
- **fusion_policy** / **retriever_params** — JSON-editor advanced
  knobs (require RAG tuning experience)
- **query_image** + **query_mode** — image / mixed retrieval

## Breaking changes

The `RetrievalStrategy` cross-domain struct cleanup has side effects
beyond the workflow node:

- **Legacy knowledge backend** (`KNOWLEDGE_BACKEND=legacy`):
  query-rewrite, NL2SQL, MinScore filtering, and per-doc filtering are
  no longer applied. The pipeline still runs; affected nodes lose those
  features. (Most production users have moved to
  `KNOWLEDGE_BACKEND=rag` and are unaffected.)
- **Agent retriever**: `UseRewrite`, `UseNl2sql`, and `MinScore` on
  `bot_common.RecallStrategy` are no longer applied at retrieval time.
  IDL field definitions are preserved; only the consumer logic is
  removed.
- **LLM workflow node's knowledge recall**: same as agent retriever —
  `MinScore`, `UseRewrite`, `UseNL2SQL` settings have no effect.

If you depend on any of the above on a non-rag path, raise the issue;
the cleanest path forward is migrating to `KNOWLEDGE_BACKEND=rag`.

## Migration

No data migration required. Existing workflow JSON loads cleanly:
legacy `useRewrite` / `useRerank` / `useNl2sql` / `isPersonalOnly` /
`minScore` / `documentIDs` params are silently ignored by the new
`Adapt`. On first re-save, those legacy keys disappear from the JSON.

## Verification

- `go build ./...` passes
- ragimpl, workflow node, agentflow, contract test suites all PASS
- Frontend dataset-setting refactored: index.tsx is now a ~80-line
  orchestrator delegating to 5 section components.
- Wire body smoke test: see manual checklist in
  `docs/superpowers/specs/2026-05-20-knowledge-retrieve-param-alignment-design.md`
