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

import React from 'react';

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

// DocumentStatus enum values mirror @coze-arch/idl/knowledge (see common.ts):
//   Processing = 0
//   Enable     = 1  // "ready"
//   Failed     = 9
const STATUS_PROCESSING = 0;
const STATUS_READY = 1;
const STATUS_FAILED = 9;

const mockGetProgress = vi.fn();
const mockRetryDocument = vi.fn();
const mockToastError = vi.fn();

vi.mock('@coze-arch/bot-api', () => ({
  KnowledgeApi: {
    // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
    GetDocumentProgress: (...args: unknown[]) => mockGetProgress(...args),
    // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
    RetryDocument: (...args: unknown[]) => mockRetryDocument(...args),
  },
}));

// The real @coze-arch/bot-semi Progress pulls in CSS that vitest can't
// resolve in this package's environment. The contract under test is the
// polling/aggregation/error-rendering logic, not the design-system primitives.
vi.mock('@coze-arch/bot-semi', () => ({
  // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
  Progress: ({ percent }: { percent: number }) => (
    <div data-testid="progress" data-percent={percent} />
  ),
}));

vi.mock('@coze-arch/coze-design', () => ({
  // eslint-disable-next-line @typescript-eslint/naming-convention -- mirror upstream PascalCase exports
  Button: ({
    children,
    disabled,
    onClick,
  }: {
    children: React.ReactNode;
    disabled?: boolean;
    onClick?: () => void;
  }) => (
    <button disabled={disabled} type="button" onClick={onClick}>
      {children}
    </button>
  ),
  // Toast.error is invoked on retry failure; the test asserts it via the
  // mock fn so we don't depend on the real coze-design Toast surface.
  Toast: {
    error: (...args: unknown[]) => mockToastError(...args),
  },
}));

import { UploadProgressPoll } from './index';

describe('<UploadProgressPoll />', () => {
  beforeEach(() => {
    mockGetProgress.mockReset();
    mockRetryDocument.mockReset();
    mockToastError.mockReset();
  });

  it('polls until all docs are ready then calls onComplete', async () => {
    mockGetProgress
      .mockResolvedValueOnce({
        data: [{ document_id: 'd1', status: STATUS_PROCESSING, progress: 30 }],
      })
      .mockResolvedValueOnce({
        data: [{ document_id: 'd1', status: STATUS_PROCESSING, progress: 70 }],
      })
      .mockResolvedValueOnce({
        data: [{ document_id: 'd1', status: STATUS_READY, progress: 100 }],
      });

    const onComplete = vi.fn();
    render(
      <UploadProgressPoll
        docIds={['d1']}
        onComplete={onComplete}
        pollIntervalMs={5}
      />,
    );

    await waitFor(() => expect(onComplete).toHaveBeenCalledTimes(1), {
      timeout: 2000,
    });
    expect(screen.getByText('1/1')).toBeDefined();
  });

  it('shows the rag-supplied error string and enabled 重试 CTA when a doc fails', async () => {
    mockGetProgress.mockResolvedValue({
      data: [
        {
          document_id: 'd1',
          status: STATUS_FAILED,
          progress: 0,
          status_descript: 'rate limit hit',
        },
      ],
    });

    const onComplete = vi.fn();
    render(
      <UploadProgressPoll
        docIds={['d1']}
        onComplete={onComplete}
        pollIntervalMs={5}
      />,
    );

    await waitFor(() =>
      expect(screen.queryByText(/rate limit hit/)).not.toBeNull(),
    );
    const cta = screen.getByText('重试') as HTMLElement;
    expect(cta).toBeDefined();
    // CTA must be enabled — RetryDocument is now wired through (R2-D-fe-Retry).
    expect((cta.closest('button') as HTMLButtonElement).disabled).toBe(false);
    expect(onComplete).not.toHaveBeenCalled();
  });

  it('clicking 重试 calls KnowledgeApi.RetryDocument and clears progress state for that doc', async () => {
    // First poll surfaces a failed doc d1. After the retry click we expect
    // KnowledgeApi.RetryDocument({document_id: 'd1'}) to be invoked once,
    // and the local progress for d1 to be cleared so the next polling tick
    // repopulates it (the server bumped rag_doc_mapping.last_task_id so
    // MGetDocumentProgress now follows the retry's new task).
    mockGetProgress.mockResolvedValue({
      data: [
        {
          document_id: 'd1',
          status: STATUS_FAILED,
          progress: 0,
          status_descript: 'rate limit hit',
        },
      ],
    });
    mockRetryDocument.mockResolvedValue({
      document_info: { document_id: 'd1' },
    });

    const onComplete = vi.fn();
    render(
      <UploadProgressPoll
        docIds={['d1']}
        onComplete={onComplete}
        pollIntervalMs={5}
      />,
    );

    await waitFor(() => expect(screen.queryByText('重试')).not.toBeNull());
    const button = screen
      .getByText('重试')
      .closest('button') as HTMLButtonElement;
    fireEvent.click(button);

    await waitFor(() => {
      expect(mockRetryDocument).toHaveBeenCalledWith({ document_id: 'd1' });
    });
    expect(mockRetryDocument).toHaveBeenCalledTimes(1);
    expect(mockToastError).not.toHaveBeenCalled();
  });

  it('shows a toast when RetryDocument fails', async () => {
    mockGetProgress.mockResolvedValue({
      data: [
        {
          document_id: 'd1',
          status: STATUS_FAILED,
          progress: 0,
          status_descript: 'rate limit hit',
        },
      ],
    });
    mockRetryDocument.mockRejectedValue(new Error('network blip'));

    const onComplete = vi.fn();
    render(
      <UploadProgressPoll
        docIds={['d1']}
        onComplete={onComplete}
        pollIntervalMs={5}
      />,
    );

    await waitFor(() => expect(screen.queryByText('重试')).not.toBeNull());
    const button = screen
      .getByText('重试')
      .closest('button') as HTMLButtonElement;
    fireEvent.click(button);

    await waitFor(() => expect(mockToastError).toHaveBeenCalledTimes(1));
    expect(mockRetryDocument).toHaveBeenCalledWith({ document_id: 'd1' });
  });

  it('aggregates "N/M ready" across multiple docs', async () => {
    mockGetProgress.mockResolvedValue({
      data: [
        { document_id: 'd1', status: STATUS_READY, progress: 100 },
        { document_id: 'd2', status: STATUS_PROCESSING, progress: 50 },
        { document_id: 'd3', status: STATUS_PROCESSING, progress: 20 },
      ],
    });

    const onComplete = vi.fn();
    render(
      <UploadProgressPoll
        docIds={['d1', 'd2', 'd3']}
        onComplete={onComplete}
        pollIntervalMs={5}
      />,
    );

    await waitFor(() => expect(screen.queryByText('1/3')).not.toBeNull());
    expect(onComplete).not.toHaveBeenCalled();
  });

  it('does not fire onComplete on empty docIds', async () => {
    // [].every(...) is vacuously true. A buggy version would fire
    // onComplete on the very first tick before any doc exists. We also
    // expect zero GetDocumentProgress requests — polling { document_ids:
    // [] } is wasted work and rejected by some backends.
    const onComplete = vi.fn();
    render(
      <UploadProgressPoll
        docIds={[]}
        onComplete={onComplete}
        pollIntervalMs={5}
      />,
    );
    await new Promise(r => setTimeout(r, 50));
    expect(onComplete).not.toHaveBeenCalled();
    expect(mockGetProgress).not.toHaveBeenCalled();
  });

  it('does not refire poll immediately on parent rerender with a fresh same-content docIds reference', async () => {
    // A buggy version (effect depending on the array identity of docIds)
    // would tear down + re-init the effect on every parent rerender that
    // passes a new array literal, synchronously firing a 2nd
    // GetDocumentProgress request. With the ref + join-key fix the effect
    // is stable across rerenders that don't actually change the contents.
    mockGetProgress.mockResolvedValue({
      data: [{ document_id: 'd1', status: STATUS_PROCESSING, progress: 50 }],
    });

    const onComplete = vi.fn();
    // Use a large pollIntervalMs so the only way to see >1 call within the
    // test window is the buggy effect-restart path.
    const { rerender } = render(
      <UploadProgressPoll
        docIds={['d1']}
        onComplete={onComplete}
        pollIntervalMs={5000}
      />,
    );
    await waitFor(() => expect(mockGetProgress).toHaveBeenCalledTimes(1));

    // Rerender with a fresh array literal — different reference, same content.
    rerender(
      <UploadProgressPoll
        docIds={['d1']}
        onComplete={onComplete}
        pollIntervalMs={5000}
      />,
    );

    // No wait: a buggy version would have synchronously fired a 2nd
    // immediate request as the effect cleanup + re-init runs right after
    // the rerender commit.
    expect(mockGetProgress).toHaveBeenCalledTimes(1);
  });
});
