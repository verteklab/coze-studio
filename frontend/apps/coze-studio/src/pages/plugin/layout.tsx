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

import { Outlet, useNavigate, useParams } from 'react-router-dom';
import { useMemo } from 'react';

import { pluginResourceNavigate } from '@coze-studio/workspace-base';
import { BotPluginStoreProvider } from '@coze-studio/bot-plugin-store';
import { IconCozArrowLeft } from '@coze-arch/coze-design/icons';
import { Button } from '@coze-arch/coze-design';

const WORKFLOW_RETURN_URL_KEY = 'workflowReturnUrl';

const isEmbeddedInIframe = () => {
  if (typeof window === 'undefined') {
    return false;
  }
  try {
    return window.self !== window.top;
  } catch (error) {
    void error;
    return true;
  }
};

// 工作流页跳转插件详情时透传的 returnUrl：
// 优先 URL query，其次 sessionStorage / localStorage（容错部分宿主限制 storage 的场景）
const readWorkflowReturnUrl = (): string | null => {
  if (typeof window === 'undefined') {
    return null;
  }
  try {
    const params = new URLSearchParams(window.location.search);
    const fromQuery = params.get('returnUrl');
    if (fromQuery) {
      return decodeURIComponent(fromQuery);
    }
  } catch (error) {
    void error;
  }
  try {
    const v = sessionStorage.getItem(WORKFLOW_RETURN_URL_KEY);
    if (v) {
      return v;
    }
  } catch (error) {
    void error;
  }
  try {
    return localStorage.getItem(WORKFLOW_RETURN_URL_KEY);
  } catch (error) {
    void error;
    return null;
  }
};

const clearWorkflowReturnUrl = () => {
  try {
    sessionStorage.removeItem(WORKFLOW_RETURN_URL_KEY);
  } catch (error) {
    void error;
  }
  try {
    localStorage.removeItem(WORKFLOW_RETURN_URL_KEY);
  } catch (error) {
    void error;
  }
};

const SpaceLayout = () => {
  const { plugin_id, space_id } = useParams();
  const navBase = `/space/${space_id}`;
  const navigate = useNavigate();

  // 仅在 iframe 嵌入场景下展示返回入口，避免影响主站逻辑
  const returnUrl = useMemo(
    () => (isEmbeddedInIframe() ? readWorkflowReturnUrl() : null),
    [],
  );

  if (!plugin_id || !space_id) {
    throw Error('[plugin render error]: need plugin id and space id');
  }

  const handleBack = () => {
    if (!returnUrl) {
      return;
    }
    clearWorkflowReturnUrl();
    // 走 SPA 路由，避免在 iframe 内整页跳出
    window.history.pushState({}, '', returnUrl);
    window.dispatchEvent(new PopStateEvent('popstate'));
  };

  return (
    <BotPluginStoreProvider
      pluginID={plugin_id}
      spaceID={space_id}
      resourceNavigate={pluginResourceNavigate(navBase, plugin_id, navigate)}
    >
      {returnUrl ? (
        <div className="flex items-center px-[24px] pt-[12px]">
          <Button
            color="primary"
            icon={<IconCozArrowLeft />}
            onClick={handleBack}
          >
            返回工作流
          </Button>
        </div>
      ) : null}
      <Outlet />
    </BotPluginStoreProvider>
  );
};

export default SpaceLayout;
