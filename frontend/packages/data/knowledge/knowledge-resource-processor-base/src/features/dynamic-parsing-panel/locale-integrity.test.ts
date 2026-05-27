/*
 * Copyright 2026 coze-dev Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

import path from 'path';
import fs from 'fs';

import { describe, expect, it } from 'vitest';

const LOCALES_DIR = path.resolve(
  __dirname,
  '../../../../../../arch/resources/studio-i18n-resource/src/locales',
);

const enUS: Record<string, unknown> = JSON.parse(
  fs.readFileSync(path.join(LOCALES_DIR, 'en.json'), 'utf-8'),
);
const zhCN: Record<string, unknown> = JSON.parse(
  fs.readFileSync(path.join(LOCALES_DIR, 'zh-CN.json'), 'utf-8'),
);

const PARAM_PREFIX = 'datasets_createFileModel_rag_param_';

describe('rag-param i18n bundle integrity', () => {
  it('zh-CN and en share the same set of datasets_createFileModel_rag_param_* keys', () => {
    const enKeys = Object.keys(enUS)
      .filter(k => k.startsWith(PARAM_PREFIX))
      .sort();
    const zhKeys = Object.keys(zhCN)
      .filter(k => k.startsWith(PARAM_PREFIX))
      .sort();
    expect(zhKeys).toEqual(enKeys);
  });

  it('every datasets_createFileModel_rag_param_* value is a non-empty string', () => {
    for (const bundle of [enUS, zhCN]) {
      for (const [key, value] of Object.entries(bundle)) {
        if (!key.startsWith(PARAM_PREFIX)) {
          continue;
        }
        expect(typeof value).toBe('string');
        expect((value as string).trim().length).toBeGreaterThan(0);
      }
    }
  });
});
