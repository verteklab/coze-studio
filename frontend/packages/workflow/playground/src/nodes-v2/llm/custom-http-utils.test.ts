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

import { describe, it, expect } from 'vitest';
import { isCustomHTTPModel, isChatShapedCustomHTTP, showsLLMFields } from './custom-http-utils';

// The workflow Model interface exposes custom_http.payload_template directly
// (no nested `connection` wrapper — that shape belongs to the admin Model).
const buildModel = (template: string | undefined) =>
  ({
    custom_http: { payload_template: template ?? '' },
  }) as any;

describe('isChatShapedCustomHTTP', () => {
  it('returns true when payload_template contains {{messages}}', () => {
    const m = buildModel('{"model": "x", "payload": {"messages": {{messages}}}}');
    expect(isChatShapedCustomHTTP(m)).toBe(true);
  });

  it('returns false when payload_template uses {{image_url}}', () => {
    const m = buildModel('{"image_url": {{image_url}}}');
    expect(isChatShapedCustomHTTP(m)).toBe(false);
  });

  it('returns false for non-custom-http model', () => {
    expect(isChatShapedCustomHTTP({} as any)).toBe(false);
    expect(isChatShapedCustomHTTP(undefined)).toBe(false);
  });

  it('does not partial-match {{messages_count}}', () => {
    const m = buildModel('{"messages_count": {{messages_count}}}');
    expect(isChatShapedCustomHTTP(m)).toBe(false);
  });
});

describe('showsLLMFields', () => {
  it('returns true for built-in (non custom_http) model', () => {
    expect(showsLLMFields({} as any)).toBe(true);
  });

  it('returns true for chat-shaped custom_http', () => {
    const m = buildModel('{{messages}}');
    expect(showsLLMFields(m)).toBe(true);
  });

  it('returns false for raw HTTP custom_http', () => {
    const m = buildModel('{{image_url}}');
    expect(showsLLMFields(m)).toBe(false);
  });
});
