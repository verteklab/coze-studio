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

import { useCallback, useEffect, useRef, useState } from 'react';

import { Button, Toast } from '@coze-arch/coze-design';
import { Progress } from '@coze-arch/bot-semi';
import { KnowledgeApi } from '@coze-arch/bot-api';

import styles from './index.module.less';

const DEFAULT_POLL_MS = 2000;

// Mirrors @coze-arch/idl/knowledge DocumentStatus. Kept as named constants
// here so the JSX below reads clearly and tests don't depend on the IDL
// transitively.
//   Processing = 0
//   Enable     = 1  // success / "ready"
//   Failed     = 9
//   AuditFailed = 1000
const STATUS_READY = 1;
const STATUS_FAILED = 9;
const STATUS_AUDIT_FAILED = 1000;

interface DocProgress {
  document_id?: string;
  status?: number;
  progress?: number;
  status_descript?: string;
  document_name?: string;
}

export interface UploadProgressPollProps {
  /** Document IDs returned by the upload request — these are polled. */
  docIds: string[];
  /** Called exactly once when every doc reaches the "ready" status. */
  onComplete: () => void;
  /** Polling interval in ms. Defaults to 2s; tests override to keep fast. */
  pollIntervalMs?: number;
}

const isReady = (status?: number) => status === STATUS_READY;
const isFailed = (status?: number) =>
  status === STATUS_FAILED || status === STATUS_AUDIT_FAILED;

/**
 * Shared progress-polling UI for rag-mode upload wizards (text / image /
 * table). Polls `GetDocumentProgress` every `pollIntervalMs`, renders a
 * per-doc list and an aggregate `N/M` header, and fires `onComplete` once
 * every doc has reached the ready status.
 *
 * Failed docs surface the rag-supplied `status_descript` plus a `重试` button
 * that calls `KnowledgeApi.RetryDocument`. On success, the local progress
 * state for that doc is cleared so the next polling tick picks up the new
 * task via the server-side mapping update (ragimpl bumps
 * rag_doc_mapping.last_task_id so MGetDocumentProgress follows the retry's
 * new task). Failure surfaces as a toast.
 */
export const UploadProgressPoll = ({
  docIds,
  onComplete,
  pollIntervalMs = DEFAULT_POLL_MS,
}: UploadProgressPollProps) => {
  const [progress, setProgress] = useState<Record<string, DocProgress>>({});
  const completedRef = useRef(false);
  // Capture onComplete in a ref so changing the callback identity doesn't
  // tear down the polling loop mid-flight.
  const onCompleteRef = useRef(onComplete);
  onCompleteRef.current = onComplete;
  // Capture docIds in a ref too so an unmemoized array literal at the call
  // site (e.g. <UploadProgressPoll docIds={['d1']} />) doesn't tear down +
  // re-run the effect on every parent render — that would synchronously
  // fire a fresh GetDocumentProgress request on each rerender. The effect
  // depends on a stable join key for actual content change detection.
  const docIdsRef = useRef(docIds);
  docIdsRef.current = docIds;
  const docIdsKey = docIds.join(',');

  useEffect(() => {
    let cancelled = false;
    let timeoutId: ReturnType<typeof setTimeout> | undefined;

    const tick = async () => {
      if (cancelled) {
        return;
      }
      const currentDocIds = docIdsRef.current;
      // Nothing to poll yet — caller may still be populating docIds
      // asynchronously (e.g. waiting on a CreateDocument response).
      // Skip the wasted (and potentially error-prone) `document_ids: []`
      // request and re-check on the next tick. Critically, also skip the
      // allReady evaluation: [].every(...) is vacuously true and would
      // fire onComplete before any doc exists.
      if (currentDocIds.length === 0) {
        if (!completedRef.current) {
          timeoutId = setTimeout(tick, pollIntervalMs);
        }
        return;
      }
      try {
        const resp = await KnowledgeApi.GetDocumentProgress({
          document_ids: currentDocIds,
        });
        if (cancelled) {
          return;
        }
        const next: Record<string, DocProgress> = {};
        for (const row of resp.data ?? []) {
          if (row.document_id) {
            next[row.document_id] = row;
          }
        }
        setProgress(next);

        const allReady =
          currentDocIds.length > 0 &&
          currentDocIds.every(id => isReady(next[id]?.status));
        if (allReady && !completedRef.current) {
          completedRef.current = true;
          onCompleteRef.current();
          return; // stop polling
        }
      } catch (err) {
        // Transient network failure — next tick will retry. The server
        // has its own timeout; we intentionally don't add a client one.
        // Surfacing the error in the UI would be noisy for transient blips,
        // and a persistent failure will eventually show as no progress
        // updates (the doc stays Processing) which the user can spot.
        void err;
      }
      if (!cancelled && !completedRef.current) {
        timeoutId = setTimeout(tick, pollIntervalMs);
      }
    };

    void tick();

    return () => {
      cancelled = true;
      if (timeoutId !== undefined) {
        clearTimeout(timeoutId);
      }
    };
  }, [docIdsKey, pollIntervalMs]);

  const readyCount = docIds.filter(id => isReady(progress[id]?.status)).length;

  // handleRetry calls the RetryDocument RPC and clears the failed doc from
  // local progress state. The next polling tick repopulates the row with the
  // new task's status — the server bumped rag_doc_mapping.last_task_id so
  // MGetDocumentProgress now reflects the retry. completedRef is re-armed in
  // case onComplete had already fired and the user retried after that.
  const handleRetry = useCallback(async (docId: string) => {
    try {
      await KnowledgeApi.RetryDocument({ document_id: docId });
      setProgress(prev => {
        const next = { ...prev };
        delete next[docId];
        return next;
      });
      completedRef.current = false;
    } catch (_err) {
      // TODO: i18n — share the retry-fail key with the button label TODO above.
      Toast.error('重试失败，请稍后再试');
    }
  }, []);

  return (
    <div className={styles['upload-progress-poll']}>
      <div className={styles.summary}>
        {readyCount}/{docIds.length}
      </div>
      <ul className={styles.list}>
        {docIds.map(id => {
          const p = progress[id];
          const failed = isFailed(p?.status);
          return (
            <li key={id} className={styles.row}>
              <div className={styles['doc-id']}>{p?.document_name ?? id}</div>
              <Progress percent={p?.progress ?? 0} />
              {failed ? (
                <div className={styles.error}>
                  <span className={styles['error-text']}>
                    {/* TODO: i18n — wait for spec to register a key. */}
                    {p?.status_descript ?? '上传失败'}
                  </span>
                  {/* TODO: i18n — wait for spec to register a key. */}
                  <Button onClick={() => handleRetry(id)}>重试</Button>
                </div>
              ) : null}
            </li>
          );
        })}
      </ul>
    </div>
  );
};
