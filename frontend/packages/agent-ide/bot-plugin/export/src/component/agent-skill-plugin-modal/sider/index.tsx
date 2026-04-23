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

import { useNavigate } from 'react-router-dom';
import { type FC } from 'react';

import { useDebounceFn } from 'ahooks';
import { UISearch } from '@coze-studio/components';
import { I18n } from '@coze-arch/i18n';
import { useSpaceStore } from '@coze-arch/bot-studio-store';
import { UIButton, UICompositionModalSider } from '@coze-arch/bot-semi';
import { type From, type PluginQuery } from '@coze-agent-ide/plugin-shared';
import { PluginFilter } from '@coze-agent-ide/plugin-modal-adapter';

import s from './index.module.less';

export interface PluginModalSiderProp {
  query: PluginQuery;
  setQuery: (value: Partial<PluginQuery>, refreshPage?: boolean) => void;
  from?: From;
  // 兼容旧 props，目前点击「创建插件」直接跳到资源库页面，不再触发该回调
  onCreateSuccess?: (val?: { spaceId?: string; pluginId?: string }) => void;
  isShowStorePlugin?: boolean;
  hideCreateBtn?: boolean;
}
const MAX_SEARCH_LENGTH = 100;

// 把当前工作流页 URL 持久化，并作为 returnUrl 透传给目标页；
// 用 localStorage 兜底是因为部分宿主环境（例如受限 iframe）下 sessionStorage 可能不可用
const WORKFLOW_RETURN_URL_KEY = 'workflowReturnUrl';
const buildUrlWithReturn = (target: string) => {
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
  const sep = target.includes('?') ? '&' : '?';
  return `${target}${sep}returnUrl=${encodeURIComponent(
    returnUrlRaw,
  )}&from=workflow`;
};

export const PluginModalSider: FC<PluginModalSiderProp> = ({
  query,
  setQuery,
  from,
  isShowStorePlugin,
  hideCreateBtn,
}) => {
  const id = useSpaceStore(item => item.space.id);
  const navigate = useNavigate();
  const updateSearchQuery = (search?: string) => {
    setQuery({
      search: search ?? '',
    });
  };
  const { run: debounceChangeSearch, cancel } = useDebounceFn(
    (search: string) => {
      updateSearchQuery(search);
    },
    { wait: 300 },
  );
  const goCreatePluginPage = () => {
    navigate(buildUrlWithReturn(`/space/${id}/library?type=1`));
  };
  return (
    <UICompositionModalSider style={{ paddingTop: 16 }}>
      <UICompositionModalSider.Header>
        <UISearch
          tabIndex={-1}
          value={query.search}
          maxLength={MAX_SEARCH_LENGTH}
          onSearch={search => {
            if (!search) {
              cancel();
              updateSearchQuery(search);
            } else {
              debounceChangeSearch(search);
            }
          }}
          placeholder={I18n.t('Search')}
          data-testid="plugin.modal.search"
        />
        {hideCreateBtn ? null : (
          <UIButton
            data-testid="plugin.modal.create.plugin"
            className={s.addbtn}
            theme="solid"
            onClick={goCreatePluginPage}
          >
            {I18n.t('plugin_create')}
          </UIButton>
        )}
      </UICompositionModalSider.Header>
      <UICompositionModalSider.Content>
        <PluginFilter
          isSearching={query.search !== ''}
          type={query.type}
          onChange={type => {
            setQuery({ type });
          }}
          from={from}
          projectId={query.projectId}
          isShowStorePlugin={isShowStorePlugin}
        />
      </UICompositionModalSider.Content>
    </UICompositionModalSider>
  );
};
