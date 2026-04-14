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

const isEmbeddedInIframe = () => {
  if (typeof window === 'undefined') {
    return false;
  }
  try {
    return window.self !== window.top;
  } catch {
    return true;
  }
};

const getTemplateIdFromQuery = () => {
  if (typeof window === 'undefined') {
    return '';
  }
  return new URLSearchParams(window.location.search).get('template_id') ?? '';
};

const getIframeTypeFromQuery = () => {
  if (typeof window === 'undefined') {
    return '';
  }
  return new URLSearchParams(window.location.search).get('iframeType') ?? '';
};

export const isLookIframeMode = () => getIframeTypeFromQuery() === 'look';

const getTemplateKnowledgeApiHost = () => {
  if (typeof window === 'undefined') {
    return '';
  }
  const queryHost =
    new URLSearchParams(window.location.search).get('templateApiHost') ?? '';
  if (queryHost) {
    return queryHost;
  }
  return 'http://117.59.171.81:8000';
};

export const shouldUseTemplateKnowledgeApi = () =>
  isEmbeddedInIframe() &&
  isLookIframeMode() &&
  Boolean(getTemplateIdFromQuery());

export const requestTemplateKnowledgeApi = async <T>(
  path: string,
  body: Record<string, unknown>,
): Promise<T> => {
  const templateId = getTemplateIdFromQuery();
  const host = getTemplateKnowledgeApiHost();
  if (!templateId) {
    throw new Error('template_id is required in iframe mode');
  }

  const response = await fetch(
    `${host}/templates/${templateId}/knowledge/${path}`,
    {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Agw-Js-Conv': 'str',
      },
      // credentials: 'include',
      body: JSON.stringify(body),
    },
  );

  const result = (await response.json()) as T;
  if (!response.ok) {
    throw new Error('template knowledge api request failed');
  }
  return result;
};
