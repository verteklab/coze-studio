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
import { Checkbox, Input, Button } from '@coze-arch/coze-design';

import { type DataSetInfo } from '../type';
import s from '../index.module.less';
import { TitleArea } from '../components';

type ChunkType = 'text_chunk' | 'image_chunk';

export interface FilterSectionProps {
  value: DataSetInfo;
  onChange: (next: DataSetInfo) => void;
  readonly?: boolean;
}

const toggleChunkType = (
  current: ChunkType[] | undefined,
  type: ChunkType,
  checked: boolean,
): ChunkType[] => {
  const set = new Set(current ?? []);
  if (checked) {
    set.add(type);
  } else {
    set.delete(type);
  }
  return Array.from(set);
};

export const FilterSection: FC<FilterSectionProps> = ({
  value,
  onChange,
  readonly,
}) => {
  const target = (value?.target_chunk_types ?? []) as ChunkType[];
  const filters = value?.filters ?? {};
  const filterEntries = Object.entries(filters);

  return (
    <div>
      <div className={s['setting-item']}>
        <TitleArea
          title={I18n.t(
            'workflow_knowledge_target_chunk_types',
            {},
            'Chunk 类型',
          )}
          tip={I18n.t(
            'workflow_knowledge_target_chunk_types_tip',
            {},
            '限定只检索 text chunk 或 image chunk；留空由 query_mode 决定。',
          )}
        />
        <div className="flex gap-3">
          <Checkbox
            disabled={readonly}
            checked={target.includes('text_chunk')}
            onChange={e =>
              onChange({
                ...value,
                target_chunk_types: toggleChunkType(
                  target,
                  'text_chunk',
                  !!e.target.checked,
                ),
              })
            }
          >
            text_chunk
          </Checkbox>
          <Checkbox
            disabled={readonly}
            checked={target.includes('image_chunk')}
            onChange={e =>
              onChange({
                ...value,
                target_chunk_types: toggleChunkType(
                  target,
                  'image_chunk',
                  !!e.target.checked,
                ),
              })
            }
          >
            image_chunk
          </Checkbox>
        </div>
      </div>

      <div className={s['setting-item']}>
        <TitleArea
          title={I18n.t('workflow_knowledge_filters', {}, '过滤条件 (filters)')}
          tip={I18n.t(
            'workflow_knowledge_filters_tip',
            {},
            'KB metadata 字段过滤；key/value 由 KB 自身定义。',
          )}
        />
        {filterEntries.map(([k, v], idx) => (
          <div className="flex gap-2 mb-2" key={`${k}-${idx}`}>
            <Input
              disabled={readonly}
              value={k}
              placeholder="key"
              onChange={nk => {
                const next = { ...filters };
                delete next[k];
                next[nk] = v;
                onChange({ ...value, filters: next });
              }}
            />
            <Input
              disabled={readonly}
              value={typeof v === 'string' ? v : JSON.stringify(v)}
              placeholder="value"
              onChange={nv =>
                onChange({ ...value, filters: { ...filters, [k]: nv } })
              }
            />
            <Button
              type="tertiary"
              disabled={readonly}
              onClick={() => {
                const next = { ...filters };
                delete next[k];
                onChange({ ...value, filters: next });
              }}
            >
              ×
            </Button>
          </div>
        ))}
        <Button
          type="tertiary"
          disabled={readonly}
          onClick={() =>
            onChange({ ...value, filters: { ...filters, '': '' } })
          }
        >
          + {I18n.t('workflow_knowledge_filters_add', {}, '添加过滤条件')}
        </Button>
      </div>
    </div>
  );
};
