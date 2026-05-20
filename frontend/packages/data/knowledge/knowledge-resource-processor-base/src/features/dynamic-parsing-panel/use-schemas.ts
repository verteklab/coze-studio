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

import { useCallback, useEffect, useState } from 'react';

import { I18n } from '@coze-arch/i18n';

import {
  type DocumentParameterSchema,
  type ListRagDocumentParameterSchemasResponse,
} from './types';

// Module-level cache. The schemas catalog is system-wide (not tenant-scoped),
// changes only when rag itself ships a new parser/version, and is consulted
// once per upload session — caching here lets the wizard re-mount without
// hitting the proxy. A weak invalidation strategy (rebuild on full reload)
// is acceptable because rag-side schema rollouts are coordinated with the
// frontend bundle anyway.
let cached: DocumentParameterSchema[] | null = null;
let inflight: Promise<DocumentParameterSchema[]> | null = null;

// Frontend-side defaults synthesised for schema parameters where rag itself
// declares `default=null` but the deployment ships a sensible value. Keyed by
// parameter name; only applied when rag's default is null/undefined so a
// future rag-side default override wins without code changes.
//
// `ocr_model_id` is the only entry today: rag declares it required with
// default=null across image_document and scanned_document, but the docker
// rag stack always boots with the paddle OCR model registered, so showing
// `model-ocr-paddle-infer-text` in the input matches what the user gets if
// they hit submit immediately. Long-term this should come from
// ListRagModelProviders filtered by capability (see TODO at
// dynamic-parsing-panel.tsx model-select case).
const FRONTEND_PARAM_DEFAULTS: Readonly<Record<string, unknown>> = {
  ocr_model_id: 'model-ocr-paddle-infer-text',
};

// Params whose value is locked, regardless of user input or schema default.
// Keyed by rag schema_id, then by param.name.
//
// Why: rag's image_document schema declares enable_ocr default=false, but
// coze's workflow knowledge-retrieve node only does text-in/text-out, so an
// OCR-off image upload silently produces a KB the node cannot retrieve from.
// Force OCR on at the frontend so the natural upload UX produces text_chunks.
//
// Two entry forms:
//   - visible (omits hidden, requires reason): control renders disabled with
//     a Tooltip + inline warning showing I18n.t(reason)
//   - hidden (hidden: true, no reason): control is not rendered at all; the
//     wire value is still pinned via applyForcedParams
//
// image_chunk-related entries (enable_image_embedding, produce_image_chunk)
// are hidden because the workflow knowledge-retrieve node can't consume
// image_chunks — producing them is pure waste in this UX.
type ForcedParamEntry =
  | { value: unknown; reason: string; hidden?: false }
  | { value: unknown; hidden: true };

export const FORCED_PARAMS_BY_SCHEMA: Readonly<
  Record<string, Readonly<Record<string, Readonly<ForcedParamEntry>>>>
> = {
  image_document: {
    enable_ocr: {
      value: true,
      reason: 'datasets_createFileModel_rag_forced_ocr_hint',
    },
    enable_image_embedding: { value: false, hidden: true },
    produce_image_chunk: { value: false, hidden: true },
  },
  scanned_document: {
    enable_ocr: {
      value: true,
      reason: 'datasets_createFileModel_rag_forced_ocr_hint',
    },
    enable_image_embedding: { value: false, hidden: true },
    produce_image_chunk: { value: false, hidden: true },
  },
};

// Mutates `schemas` in place to fill in frontend defaults for parameters
// rag didn't supply one for. Safe to call on the cached array because the
// catalog is read-only after first fetch — the synthesised value lives
// alongside rag's own defaults for the rest of the session.
function applyFrontendDefaults(
  schemas: DocumentParameterSchema[],
): DocumentParameterSchema[] {
  for (const s of schemas) {
    for (const p of s.parameters) {
      if (p.default !== undefined && p.default !== null) {
        continue;
      }
      const synthesised = FRONTEND_PARAM_DEFAULTS[p.name];
      if (synthesised !== undefined) {
        p.default = synthesised;
      }
    }
  }
  return schemas;
}

async function fetchSchemas(): Promise<DocumentParameterSchema[]> {
  // The rag proxy endpoint returns a raw {schemas: [...]} envelope (no
  // {code, msg, data}) — mirrors the ListRagModelProviders fetch precedent
  // in create-knowledge-modal-v2. axiosInstance's response interceptor
  // would reject anything without `code === 0`, so we use raw fetch.
  const res = await fetch('/api/knowledge/rag/document_parameter_schemas', {
    method: 'GET',
    credentials: 'same-origin',
  });
  if (!res.ok) {
    // 401 here means "session expired" — let the upload UI surface a generic
    // error. The dynamic form's caller falls back to default values when
    // schemas can't load.
    throw new Error(`document_parameter_schemas: HTTP ${res.status}`);
  }
  const body = (await res.json()) as ListRagDocumentParameterSchemasResponse;
  return applyFrontendDefaults(body.schemas ?? []);
}

/**
 * Hook returning rag's per-file-type parameter schemas catalog. State shape
 * mirrors react-query's `{data, loading, error}` without taking the
 * dependency — the cache lives at module scope, so a hook re-invocation
 * after the first successful fetch resolves synchronously.
 *
 * `retry()` clears the cache and re-fires the fetch. Wire it to the segment
 * step's "retry" button so a transient catalog outage is recoverable
 * without forcing the user to back out and re-enter the wizard.
 */
export function useRagDocumentParameterSchemas(): {
  schemas: DocumentParameterSchema[] | null;
  loading: boolean;
  error: Error | null;
  retry: () => void;
} {
  const [schemas, setSchemas] = useState<DocumentParameterSchema[] | null>(
    cached,
  );
  const [error, setError] = useState<Error | null>(null);
  const [loading, setLoading] = useState<boolean>(cached === null);
  // Bumped by retry() to re-trigger the effect; needs to live in state so
  // the effect's dep array sees it change.
  const [tick, setTick] = useState(0);

  useEffect(() => {
    if (cached !== null) {
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError(null);
    const promise = inflight ?? (inflight = fetchSchemas());
    promise
      .then(result => {
        if (cancelled) {
          return;
        }
        cached = result;
        inflight = null;
        setSchemas(result);
        setLoading(false);
      })
      .catch((err: Error) => {
        if (cancelled) {
          return;
        }
        // Reset inflight so a retry can refire; cached stays null so the
        // next mount or retry re-fetches.
        inflight = null;
        setError(err);
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [tick]);

  const retry = useCallback(() => {
    cached = null;
    inflight = null;
    setTick(t => t + 1);
  }, []);

  return { schemas, loading, error, retry };
}

/**
 * Picks the schemas matching a given file extension. Returns ALL matches
 * (PDFs have two: pdf_text_document and scanned_document; image files have
 * two too). The caller uses this to render a schema selector when
 * `length > 1` and just auto-uses the only entry otherwise.
 */
export function matchSchemasForFile(
  schemas: DocumentParameterSchema[],
  fileType: string,
): DocumentParameterSchema[] {
  const lc = fileType.toLowerCase();
  return schemas.filter(s =>
    s.file_types.map(t => t.toLowerCase()).includes(lc),
  );
}

// i18n keys keyed by rag's schema_id. Stable bidirectional mapping — adding
// a new schema requires a new key here AND in the locale files. The fallback
// returns the raw schema_id so a missing locale entry surfaces visibly
// instead of rendering as a blank dropdown.
const SCHEMA_LABEL_KEYS: Readonly<Record<string, string>> = {
  text_document: 'datasets_createFileModel_rag_schema_text_document',
  markdown_document: 'datasets_createFileModel_rag_schema_markdown_document',
  pdf_text_document: 'datasets_createFileModel_rag_schema_pdf_text_document',
  docx_document: 'datasets_createFileModel_rag_schema_docx_document',
  image_document: 'datasets_createFileModel_rag_schema_image_document',
  scanned_document: 'datasets_createFileModel_rag_schema_scanned_document',
};

/**
 * Resolves a rag schema_id to its localised display label. Unknown ids
 * fall through to the raw schema_id so an operator can still tell which
 * schema is in play without a localisation entry; that also makes the
 * mapping table the single source of truth without a runtime crash.
 */
export function schemaLabel(schemaId: string): string {
  const key = SCHEMA_LABEL_KEYS[schemaId];
  return key ? I18n.t(key) : schemaId;
}
