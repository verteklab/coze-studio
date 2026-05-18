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

import { useEffect, useState } from 'react';

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
  return body.schemas ?? [];
}

/**
 * Hook returning rag's per-file-type parameter schemas catalog. State shape
 * mirrors react-query's `{data, loading, error}` without taking the
 * dependency — the cache lives at module scope, so a hook re-invocation
 * after the first successful fetch resolves synchronously.
 */
export function useRagDocumentParameterSchemas(): {
  schemas: DocumentParameterSchema[] | null;
  loading: boolean;
  error: Error | null;
} {
  const [schemas, setSchemas] = useState<DocumentParameterSchema[] | null>(
    cached,
  );
  const [error, setError] = useState<Error | null>(null);
  const [loading, setLoading] = useState<boolean>(cached === null);

  useEffect(() => {
    if (cached !== null) {
      return;
    }
    let cancelled = false;
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
        // Reset inflight so a retry on remount can refire; cached stays null.
        inflight = null;
        setError(err);
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return { schemas, loading, error };
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
