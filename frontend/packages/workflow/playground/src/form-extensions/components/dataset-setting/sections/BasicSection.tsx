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
import { type FC } from 'react';

import { I18n } from '@coze-arch/i18n';

import { type DataSetInfo, type Strategy } from '../type';
import s from '../index.module.less';
import { TitleArea, SliderArea, SearchStrategy } from '../components';

const DEFAULT_TOP_K = 10;

export interface BasicSectionProps {
  value: DataSetInfo;
  onChange: (next: DataSetInfo) => void;
  readonly?: boolean;
  disabled?: boolean;
}

export const BasicSection: FC<BasicSectionProps> = ({
  value,
  onChange,
  readonly,
  disabled,
}) => {
  const strategy = value?.strategy;
  const topK = value?.top_k;

  return (
    <>
      <div className={s['setting-item']}>
        <TitleArea
          title={I18n.t('knowledge_search_strategy_title')}
          tip={I18n.t('knowledge_search_strategy_tooltip')}
        />
        <SearchStrategy
          readonly={readonly || disabled}
          value={strategy as Strategy}
          onChange={v => onChange({ ...value, strategy: v })}
        />
      </div>

      <div className={s['setting-item']}>
        <TitleArea
          title={I18n.t('dataset_max_recall')}
          tip={I18n.t('bot_edit_datasetsSettings_MaxTip')}
        />
        <SliderArea
          min={1}
          max={50}
          step={1}
          value={topK}
          customStyles={{
            sliderAreaStyle: { width: '160px' },
            boundaryStyle: { width: '158px', margin: 0 },
          }}
          isDataSet
          marks={{
            markKey: DEFAULT_TOP_K,
            markText: <span className="ml-2">Default</span>,
          }}
          onChange={v => onChange({ ...value, top_k: v })}
          onClickDefault={() => onChange({ ...value, top_k: DEFAULT_TOP_K })}
          disabled={readonly || disabled}
        />
      </div>
    </>
  );
};
