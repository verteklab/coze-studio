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

import { useSearchParams } from 'react-router-dom';
import React from 'react';

import { type ApiNodeIdentifier } from '@coze-workflow/nodes';
import { I18n } from '@coze-arch/i18n';
import { IconCozLoading, IconCozExit } from '@coze-arch/coze-design/icons';
import { PluginFrom } from '@coze-arch/bot-api/playground_api';
import { PluginProductStatus } from '@coze-arch/bot-api/developer_api';

import { useGlobalState } from '@/hooks';

import { usePluginDetail } from '../hooks/use-plugin-detail';
import { usePluginNodeServiceStore } from '../hooks';

function getJumpDetailText() {
  return I18n.t('mkpl_plugin_detail', {}, 'Plugin Detail');
}

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

// 与 `agent-skill-plugin-modal` 保持一致的 returnUrl 约定，
// 让插件详情页可在 iframe 内渲染「返回工作流」按钮
const WORKFLOW_RETURN_URL_KEY = 'workflowReturnUrl';

const persistWorkflowReturnUrl = () => {
  const { pathname, search, hash } = window.location;
  const returnUrlRaw = `${pathname}${search}${hash}`;
  try {
    sessionStorage.setItem(WORKFLOW_RETURN_URL_KEY, returnUrlRaw);
  } catch (error) {
    void error;
  }
  try {
    localStorage.setItem(WORKFLOW_RETURN_URL_KEY, returnUrlRaw);
  } catch (error) {
    void error;
  }
  return returnUrlRaw;
};

const appendReturnUrl = (target: string, returnUrlRaw: string) => {
  const sep = target.includes('?') ? '&' : '?';
  return `${target}${sep}returnUrl=${encodeURIComponent(
    returnUrlRaw,
  )}&from=workflow`;
};

const openPluginDetail = (url: string, options?: { external?: boolean }) => {
  if (!isEmbeddedInIframe()) {
    window.open(url, '_blank');
    return;
  }
  // 外站（如开源版 SaaS 商店）属于跨域，无法用 SPA 返回，
  // 这里仅做同窗口跳转，不附 returnUrl
  if (options?.external) {
    window.location.href = url;
    return;
  }
  const returnUrlRaw = persistWorkflowReturnUrl();
  window.location.href = appendReturnUrl(url, returnUrlRaw);
};

export const PluginLink = ({
  identifier,
}: {
  identifier: ApiNodeIdentifier;
}) => {
  const { getApiNodeDetail } = usePluginNodeServiceStore(state => ({
    getApiNodeDetail: state.getData,
  }));
  const { getProjectApi } = useGlobalState();
  const [searchParams] = useSearchParams();
  const spaceIdFromUrl = searchParams.get('space_id');
  const apiNodeDetail = getApiNodeDetail(identifier);

  const {
    spaceID: spaceId,
    pluginID: pluginId,
    projectID: projectId,
    pluginProductStatus,
    plugin_from,
  } = apiNodeDetail || {};

  // In the same space, and the status is to be submitted plug-ins, jump directly /space/xxx/plugin/yyy
  const noNeedQuery =
    spaceIdFromUrl === spaceId &&
    pluginProductStatus === PluginProductStatus.Default;

  const { isLoading, storePluginId } = usePluginDetail({
    pluginId: pluginId || '',
    needQuery: !noNeedQuery,
  });

  // The operation and maintenance platform does not need to display plug-ins, jump links
  if (IS_BOT_OP) {
    return null;
  }

  if (isLoading) {
    return <IconCozLoading className="animate-spin text-xs coz-fg-dim" />;
  }

  const handleClick = (e: React.MouseEvent<HTMLSpanElement, MouseEvent>) => {
    const ideNavigate = getProjectApi()?.navigate;
    if (IS_OPEN_SOURCE && plugin_from === PluginFrom.FromSaas) {
      const url = window.atob('aHR0cHM6Ly93d3cuY296ZS5jbg==');
      openPluginDetail(`${url}/store/plugin/${pluginId}?plugin_id=true`, {
        external: true,
      });
    } else if (projectId && projectId !== '0' && ideNavigate) {
      ideNavigate(`/plugin/${pluginId}`);
    } else {
      const url =
        storePluginId && !noNeedQuery
          ? `/store/plugin/${storePluginId}` // Other status (on the shelves, off the shelves, under review)
          : `/space/${spaceId}/plugin/${pluginId}`; // Not on the shelves
      openPluginDetail(url);
    }
    e.stopPropagation();
  };

  return (
    <span
      className="cursor-pointer flex items-center w-full justify-between"
      onClick={handleClick}
    >
      {getJumpDetailText()}
      <IconCozExit className="text-xs" />
    </span>
  );
};

export const createPluginLink = (identifier: ApiNodeIdentifier) => (
  <PluginLink identifier={identifier} />
);
