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

import { useRef, type RefObject } from 'react';

import cls from 'classnames';
import { usePlayground } from '@flowgram-adapter/free-layout-editor';
import { Divider } from '@coze-arch/coze-design';

import { useTemplateService } from '@/hooks/use-template-service';

import type { ITool } from '../type';
import { StartTestRunButton } from '../../test-run/test-run-button/start-test-run-button';
import { OpenTraceButton } from '../../test-run/test-run-button/open-trace-button';
import { RoleButton } from '../../flow-role';
import { useGlobalState } from '../../../hooks';
import { Zoom } from './zoom';
import { MinimapSwitch } from './minimap-switch';
import { Interactive } from './interactive';
import { Comment } from './comment';
import { AutoLayout } from './auto-layout';
import { AddNode } from './add-node';

import css from './tools.module.less';

// 与 start-test-run-button 中的可见性规则保持一致：
// iframe 嵌入下，除非 URL 上携带 isManage=true，否则视为「只读预览」态，
// 不仅试运行按钮要隐藏，承载它的整个右侧 section（含 OpenTrace、RoleButton）一并隐藏，
// 避免空壳容器留下一个圆角矩形
const isTestRunSectionHidden = () => {
  if (typeof window === 'undefined') {
    return false;
  }
  let inIframe = false;
  try {
    inIframe = window.self !== window.top;
  } catch (error) {
    void error;
    inIframe = true;
  }
  if (!inIframe) {
    return false;
  }
  try {
    const params = new URLSearchParams(window.location.search);
    return params.get('isManage') !== 'true';
  } catch (error) {
    void error;
    return true;
  }
};

export const Tools = (props: ITool) => {
  const templateState = useTemplateService();

  const playground = usePlayground();
  const { isChatflow } = useGlobalState();
  const enableAddNode = !playground.config.readonly;
  const toolbarRef = useRef<HTMLDivElement>();
  const hideTestRunSection = isTestRunSectionHidden();
  return (
    <div
      className={cls(
        css['tools-wrap'],
        templateState.templateVisible ? 'bottom-[2px]' : 'bottom-[16px]',
      )}
      ref={toolbarRef as RefObject<HTMLDivElement>}
    >
      <div className={css['tools-section']}>
        <Interactive />
        <Zoom />
        <Comment />
        <AutoLayout />
        <MinimapSwitch {...props} />
        {enableAddNode ? (
          <>
            <Divider layout="vertical" style={{ height: '16px' }} margin={3} />
            <AddNode {...props} toolbarRef={toolbarRef} />
          </>
        ) : null}
      </div>
      {hideTestRunSection ? null : (
        <div className={cls(css['tools-section'], css['test-run'])}>
          {isChatflow ? <RoleButton /> : null}
          {/* The operation and maintenance platform does not need debugging and practice running, just need to view the information to troubleshoot problems */}
          {IS_BOT_OP ? (
            <OpenTraceButton />
          ) : (
            <>
              {isChatflow ? (
                <Divider
                  layout="vertical"
                  style={{ height: '16px' }}
                  margin={3}
                />
              ) : null}
              {/* will support soon */}
              {!IS_OPEN_SOURCE && <OpenTraceButton />}
              <StartTestRunButton />
            </>
          )}
        </div>
      )}
    </div>
  );
};
