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
import { type FC, useState } from 'react';

import { I18n } from '@coze-arch/i18n';
import { Input, Select, Collapse } from '@coze-arch/coze-design';

import { type DataSetInfo } from '../type';
import s from '../index.module.less';
import { TitleArea } from '../components';

const QUERY_MODE_OPTIONS = [
  { label: 'Text', value: 'text_input' },
  { label: 'Image', value: 'image_input' },
  { label: 'Text + Image', value: 'mixed_input' },
];

export interface QueryInputSectionProps {
  value: DataSetInfo;
  onChange: (next: DataSetInfo) => void;
  readonly?: boolean;
}

export const QueryInputSection: FC<QueryInputSectionProps> = ({
  value,
  onChange,
  readonly,
}) => {
  const [open, setOpen] = useState(false);
  const queryMode = value?.query_mode ?? 'text_input';
  const queryImage = value?.query_image;

  return (
    <Collapse
      activeKey={open ? 'qi' : undefined}
      onChange={k => setOpen(Array.isArray(k) ? k.includes('qi') : k === 'qi')}
    >
      <Collapse.Panel
        itemKey="qi"
        header={I18n.t(
          'workflow_knowledge_image_query',
          {},
          '图片查询（可选）',
        )}
      >
        <div className={s['setting-item']}>
          <TitleArea
            title={I18n.t('workflow_knowledge_query_mode', {}, '查询模式')}
          />
          <Select
            disabled={readonly}
            value={queryMode}
            optionList={QUERY_MODE_OPTIONS}
            onChange={v =>
              onChange({
                ...value,
                query_mode: v as DataSetInfo['query_mode'],
              })
            }
          />
        </div>

        {queryMode !== 'text_input' ? (
          <div className={s['setting-item']}>
            <TitleArea
              title={I18n.t(
                'workflow_knowledge_image_ref',
                {},
                '图片引用 (image_ref)',
              )}
              tip={I18n.t(
                'workflow_knowledge_image_ref_tip',
                {},
                '已上传到 rag 对象存储的引用键。',
              )}
            />
            <Input
              disabled={readonly}
              value={queryImage?.image_ref ?? ''}
              onChange={v =>
                onChange({
                  ...value,
                  query_image: { ...queryImage, image_ref: v },
                })
              }
            />
          </div>
        ) : null}
      </Collapse.Panel>
    </Collapse>
  );
};
