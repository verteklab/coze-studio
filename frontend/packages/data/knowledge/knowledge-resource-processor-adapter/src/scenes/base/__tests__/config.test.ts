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

import { describe, it, expect, vi } from 'vitest';

// happy-dom doesn't ship a canvas implementation; lottie-web (pulled in
// transitively via @coze-arch/coze-design icons used by the core package this
// SUT depends on) crashes at module init when it calls
// `document.createElement('canvas').getContext('2d')`. Stub `getContext` first
// via `vi.hoisted` so the patch lands before vitest hoists the upstream-config
// `vi.mock` factories below — which run at module-eval time and pull in
// `@coze-data/knowledge-resource-processor-core` (which loads lottie).
vi.hoisted(() => {
  if (typeof globalThis.HTMLCanvasElement !== 'undefined') {
    // lottie's feature-detect runs `ctx.fillRect(0,0,1,1)` on init; return a
    // Proxy that swallows every method/property access so any probe succeeds.
    const noop = () => undefined;
    const ctxProxy = new Proxy(
      {},
      {
        get: () => noop,
      },
    );
    globalThis.HTMLCanvasElement.prototype.getContext = (() =>
      ctxProxy) as unknown as HTMLCanvasElement['getContext'];
  }
});

// config.ts pulls in 11 upstream wizard configs whose module init reaches into
// i18n, semi/coze-design CSS, zustand stores, and other things vitest can't
// resolve here. We assert on which *sentinel* config object is returned per
// (type, opt, backend) cell — not rendering anything — so stub each upstream
// config to a unique tagged value the test can identity-check against.
//
// The sentinel pattern (rather than passing through the real config) means a
// later refactor that, say, swaps `TextLocalAddRagConfig` for a wrapper around
// `TextLocalAddUpdateConfig` would still fail this suite — exactly the
// behaviour we want to lock in for Task 10's gating contract.
vi.mock(
  '@coze-data/knowledge-resource-processor-base/features/resegment/text',
  () => ({ TextResegmentConfig: { _tag: 'TextResegmentConfig' } }),
);
vi.mock(
  '@coze-data/knowledge-resource-processor-base/features/resegment/table',
  () => ({ TableResegmentConfig: { _tag: 'TableResegmentConfig' } }),
);
vi.mock(
  '@coze-data/knowledge-resource-processor-base/features/knowledge-type/text/first-party/local/resegment',
  () => ({ TextLocalResegmentConfig: { _tag: 'TextLocalResegmentConfig' } }),
);
vi.mock(
  '@coze-data/knowledge-resource-processor-base/features/knowledge-type/text/first-party/local/add',
  () => ({ TextLocalAddUpdateConfig: { _tag: 'TextLocalAddUpdateConfig' } }),
);
vi.mock(
  '@coze-data/knowledge-resource-processor-base/features/knowledge-type/text/first-party/local/add-rag',
  () => ({ TextLocalAddRagConfig: { _tag: 'TextLocalAddRagConfig' } }),
);
vi.mock(
  '@coze-data/knowledge-resource-processor-base/features/knowledge-type/text/first-party/custom/add',
  () => ({ TextCustomAddUpdateConfig: { _tag: 'TextCustomAddUpdateConfig' } }),
);
vi.mock(
  '@coze-data/knowledge-resource-processor-base/features/knowledge-type/table/first-party/local/incremental',
  () => ({
    TableLocalIncrementalConfig: { _tag: 'TableLocalIncrementalConfig' },
  }),
);
vi.mock(
  '@coze-data/knowledge-resource-processor-base/features/knowledge-type/table/first-party/local/add',
  () => ({ TableLocalAddConfig: { _tag: 'TableLocalAddConfig' } }),
);
vi.mock(
  '@coze-data/knowledge-resource-processor-base/features/knowledge-type/table/first-party/local/add-rag',
  () => ({ TableLocalAddRagConfig: { _tag: 'TableLocalAddRagConfig' } }),
);
vi.mock(
  '@coze-data/knowledge-resource-processor-base/features/knowledge-type/table/first-party/custom/incremental',
  () => ({
    TableCustomIncrementalConfig: { _tag: 'TableCustomIncrementalConfig' },
  }),
);
vi.mock(
  '@coze-data/knowledge-resource-processor-base/features/knowledge-type/table/first-party/custom/add',
  () => ({ TableCustomAddConfig: { _tag: 'TableCustomAddConfig' } }),
);
vi.mock(
  '@coze-data/knowledge-resource-processor-base/features/knowledge-type/image/file',
  () => ({ ImageFileAddConfig: { _tag: 'ImageFileAddConfig' } }),
);
vi.mock(
  '@coze-data/knowledge-resource-processor-base/features/knowledge-type/image/file/add-rag',
  () => ({ ImageFileAddRagConfig: { _tag: 'ImageFileAddRagConfig' } }),
);

// Imported after the mocks above are registered so the SUT picks them up.
import {
  UnitType,
  OptType,
} from '@coze-data/knowledge-resource-processor-core';

import { getUploadConfig } from '../config';

describe('getUploadConfig — backend gating', () => {
  describe('TEXT_DOC ADD', () => {
    it('returns legacy when backend="legacy"', () => {
      expect(getUploadConfig(UnitType.TEXT_DOC, OptType.ADD, 'legacy')).toEqual(
        { _tag: 'TextLocalAddUpdateConfig' },
      );
    });

    it('returns legacy when backend is undefined (safe fallback)', () => {
      expect(getUploadConfig(UnitType.TEXT_DOC, OptType.ADD)).toEqual({
        _tag: 'TextLocalAddUpdateConfig',
      });
    });

    it('returns rag when backend="rag"', () => {
      expect(getUploadConfig(UnitType.TEXT_DOC, OptType.ADD, 'rag')).toEqual({
        _tag: 'TextLocalAddRagConfig',
      });
    });
  });

  describe('TABLE_DOC ADD', () => {
    it('returns legacy when backend="legacy"', () => {
      expect(
        getUploadConfig(UnitType.TABLE_DOC, OptType.ADD, 'legacy'),
      ).toEqual({ _tag: 'TableLocalAddConfig' });
    });

    it('returns legacy when backend is undefined (safe fallback)', () => {
      expect(getUploadConfig(UnitType.TABLE_DOC, OptType.ADD)).toEqual({
        _tag: 'TableLocalAddConfig',
      });
    });

    it('returns rag when backend="rag"', () => {
      expect(getUploadConfig(UnitType.TABLE_DOC, OptType.ADD, 'rag')).toEqual({
        _tag: 'TableLocalAddRagConfig',
      });
    });
  });

  describe('IMAGE_FILE ADD', () => {
    it('returns legacy when backend="legacy"', () => {
      expect(
        getUploadConfig(UnitType.IMAGE_FILE, OptType.ADD, 'legacy'),
      ).toEqual({ _tag: 'ImageFileAddConfig' });
    });

    it('returns legacy when backend is undefined (safe fallback)', () => {
      expect(getUploadConfig(UnitType.IMAGE_FILE, OptType.ADD)).toEqual({
        _tag: 'ImageFileAddConfig',
      });
    });

    it('returns rag when backend="rag"', () => {
      expect(getUploadConfig(UnitType.IMAGE_FILE, OptType.ADD, 'rag')).toEqual({
        _tag: 'ImageFileAddRagConfig',
      });
    });
  });

  it('treats unknown backend values as legacy (safe fallback)', () => {
    // Spec: only "rag" routes to rag; everything else falls back to legacy.
    // Locks in isRagBackend's strict-equality contract at the wizard layer.
    expect(getUploadConfig(UnitType.TEXT_DOC, OptType.ADD, 'mystery')).toEqual({
      _tag: 'TextLocalAddUpdateConfig',
    });
  });

  it('does not affect non-ADD entries (RESEGMENT / INCREMENTAL stay legacy in both modes)', () => {
    // The rag flag only swaps the ADD wizard for the three KB types that ship
    // a rag-mode counterpart. Resegment + incremental flows are unchanged.
    expect(
      getUploadConfig(UnitType.TEXT_DOC, OptType.RESEGMENT, 'rag'),
    ).toEqual({ _tag: 'TextLocalResegmentConfig' });
    expect(
      getUploadConfig(UnitType.TEXT_DOC, OptType.RESEGMENT, 'legacy'),
    ).toEqual({ _tag: 'TextLocalResegmentConfig' });
    expect(
      getUploadConfig(UnitType.TABLE_DOC, OptType.INCREMENTAL, 'rag'),
    ).toEqual({ _tag: 'TableLocalIncrementalConfig' });
    expect(
      getUploadConfig(UnitType.TABLE_DOC, OptType.INCREMENTAL, 'legacy'),
    ).toEqual({ _tag: 'TableLocalIncrementalConfig' });
  });

  it('returns null for unknown (type, opt) combinations regardless of backend', () => {
    // e.g., TEXT_CUSTOM has no INCREMENTAL entry.
    expect(
      getUploadConfig(UnitType.TEXT_CUSTOM, OptType.INCREMENTAL, 'rag'),
    ).toBeNull();
    expect(
      getUploadConfig(UnitType.TEXT_CUSTOM, OptType.INCREMENTAL, 'legacy'),
    ).toBeNull();
  });
});
