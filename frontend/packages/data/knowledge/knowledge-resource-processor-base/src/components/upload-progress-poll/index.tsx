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

import { useEffect, useRef, useState } from 'react';

import { Button } from '@coze-arch/coze-design';
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
 * Failed docs surface the rag-supplied `status_descript` plus a disabled
 * "联系管理员" CTA — `ragimpl.RetryDocument` is not yet wired through to the
 * rag service's `POST /documents/:id/retry` (see Task 0 of the rag-flow
 * alignment plan), so retry is intentionally unavailable for now. Once
 * that's wired through this becomes a real retry button.
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

  useEffect(() => {
    let cancelled = false;
    let timeoutId: ReturnType<typeof setTimeout> | undefined;

    const tick = async () => {
      try {
        const resp = await KnowledgeApi.GetDocumentProgress({
          document_ids: docIds,
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

        const allReady = docIds.every(id => isReady(next[id]?.status));
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
  }, [docIds, pollIntervalMs]);

  const readyCount = docIds.filter(id => isReady(progress[id]?.status)).length;

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
                  {/* RetryDocument isn't wired yet (Task 0); disabled. */}
                  <Button disabled>联系管理员</Button>
                </div>
              ) : null}
            </li>
          );
        })}
      </ul>
    </div>
  );
};
