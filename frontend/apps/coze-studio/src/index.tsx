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

import { createRoot } from 'react-dom/client';
import { initI18nInstance } from '@coze-arch/i18n/raw';
import { dynamicImportMdBoxStyle } from '@coze-arch/bot-md-box-adapter/style';
import { pullFeatureFlags, type FEATURE_FLAGS } from '@coze-arch/bot-flags';

import { App } from './app';
import './global.less';
import './index.less';

const SESSION_KEY_QUERY_PARAM = 'session_key';

const normalizeSessionKey = (rawSessionKey: string) => {
  let sessionKey = rawSessionKey.trim();
  if (!sessionKey) {
    return '';
  }

  try {
    sessionKey = decodeURIComponent(sessionKey);
  } catch (error) {
    if (!(error instanceof URIError)) {
      throw error;
    }
    // Keep original value if decoding fails.
  }

  if (sessionKey.startsWith(`${SESSION_KEY_QUERY_PARAM}=`)) {
    sessionKey = sessionKey.slice(`${SESSION_KEY_QUERY_PARAM}=`.length);
  }

  if (sessionKey.includes(';')) {
    sessionKey = sessionKey.split(';')[0]?.trim() ?? '';
  }

  if (sessionKey === 'null' || sessionKey === 'undefined') {
    return '';
  }

  return sessionKey.trim();
};

const syncSessionKeyFromUrlToCookie = () => {
  const currentURL = new URL(window.location.href);
  const rawSessionKey = currentURL.searchParams.get(SESSION_KEY_QUERY_PARAM);
  const sessionKey = rawSessionKey ? normalizeSessionKey(rawSessionKey) : '';
  if (!sessionKey) {
    return;
  }

  const isHTTPS = window.location.protocol === 'https:';
  const sameSite = isHTTPS ? 'none' : 'lax';
  const secureFlag = isHTTPS ? '; secure' : '';
  document.cookie = `${SESSION_KEY_QUERY_PARAM}=${sessionKey}; path=/; samesite=${sameSite}${secureFlag}`;

  currentURL.searchParams.delete(SESSION_KEY_QUERY_PARAM);
  window.history.replaceState(
    window.history.state,
    document.title,
    `${currentURL.pathname}${currentURL.search}${currentURL.hash}`,
  );
};

const initFlags = () => {
  pullFeatureFlags({
    timeout: 1000 * 4,
    fetchFeatureGating: () => Promise.resolve({} as unknown as FEATURE_FLAGS),
  });
};

const main = () => {
  syncSessionKeyFromUrlToCookie();
  // Initialize the value of the function switch
  initFlags();
  // Initialize i18n
  initI18nInstance({
    lng: (localStorage.getItem('i18next') ?? (IS_OVERSEA ? 'en' : 'zh-CN')) as
      | 'en'
      | 'zh-CN',
  });
  // Import mdbox styles dynamically
  dynamicImportMdBoxStyle();

  const $root = document.getElementById('root');
  if (!$root) {
    throw new Error('root element not found');
  }
  const root = createRoot($root);

  root.render(<App />);
};

main();
