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

import { type FC, useMemo, useRef } from 'react';

import {
  BaseLibraryPage,
  useDatabaseConfig,
  usePluginConfig,
  useWorkflowConfig,
  usePromptConfig,
  useKnowledgeConfig,
} from '@coze-studio/workspace-base/library';
import { IconCozArrowLeft } from '@coze-arch/coze-design/icons';
import { Button } from '@coze-arch/coze-design';

const isEmbeddedInIframe = () => {
  try {
    return window.self !== window.top;
  } catch (error) {
    void error;
    return true;
  }
};

// 工作流“创建插件”跳转时带到 URL 上的 returnUrl，
// 资源库页据此渲染「返回工作流」按钮
const getReturnUrlFromLocation = (): string | null => {
  if (typeof window === 'undefined') {
    return null;
  }
  try {
    const params = new URLSearchParams(window.location.search);
    const fromQuery = params.get('returnUrl');
    return fromQuery ? decodeURIComponent(fromQuery) : null;
  } catch (error) {
    void error;
    return null;
  }
};

export const LibraryPage: FC<{ spaceId: string }> = ({ spaceId }) => {
  const basePageRef = useRef<{ reloadList: () => void }>(null);
  const configCommonParams = {
    spaceId,
    reloadList: () => {
      basePageRef.current?.reloadList();
    },
  };
  const { config: pluginConfig, modals: pluginModals } =
    usePluginConfig(configCommonParams);
  const { config: workflowConfig, modals: workflowModals } =
    useWorkflowConfig(configCommonParams);
  const { config: knowledgeConfig, modals: knowledgeModals } =
    useKnowledgeConfig(configCommonParams);
  const { config: promptConfig, modals: promptModals } =
    usePromptConfig(configCommonParams);
  const { config: databaseConfig, modals: databaseModals } =
    useDatabaseConfig(configCommonParams);

  const returnUrl = useMemo(getReturnUrlFromLocation, []);
  // 携带 returnUrl 时，说明是从工作流弹窗跳过来创建插件的，
  // 即便处于 iframe 中也需要展示完整列表，否则插件 tab 会被裁掉
  const shouldUseKnowledgeOnly = isEmbeddedInIframe() && !returnUrl;

  const handleBack = () => {
    if (!returnUrl) {
      return;
    }
    try {
      sessionStorage.removeItem('workflowReturnUrl');
    } catch (error) {
      void error;
    }
    try {
      localStorage.removeItem('workflowReturnUrl');
    } catch (error) {
      void error;
    }
    // 走 SPA 路由，避免在 iframe 中跳出
    window.history.pushState({}, '', returnUrl);
    window.dispatchEvent(new PopStateEvent('popstate'));
  };

  return (
    <>
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
      <BaseLibraryPage
        spaceId={spaceId}
        ref={basePageRef}
        entityConfigs={
          shouldUseKnowledgeOnly
            ? [knowledgeConfig]
            : returnUrl
              ? // 从工作流弹窗跳过来时，右上角「+ 资源」只保留「插件」
                [pluginConfig]
              : [
                  pluginConfig,
                  workflowConfig,
                  knowledgeConfig,
                  promptConfig,
                  databaseConfig,
                ]
        }
      />
      {!shouldUseKnowledgeOnly ? pluginModals : null}
      {!shouldUseKnowledgeOnly && !returnUrl ? workflowModals : null}
      {!shouldUseKnowledgeOnly && !returnUrl ? promptModals : null}
      {!shouldUseKnowledgeOnly && !returnUrl ? databaseModals : null}
      {!returnUrl ? knowledgeModals : null}
    </>
  );
};
