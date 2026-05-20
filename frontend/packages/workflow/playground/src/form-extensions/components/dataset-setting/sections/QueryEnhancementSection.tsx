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

import { useNodeTestId } from '@coze-workflow/base';
import { I18n } from '@coze-arch/i18n';

import { type DataSetInfo } from '../type';
import s from '../index.module.less';
import { TitleArea } from '../components';
import { CheckboxWithLabel } from '../../checkbox-with-label';

const RAG_LLM_TOOLTIP = I18n.t(
  'workflow_knowledge_rag_llm_required',
  {},
  '需 rag 部署侧配置 llm_base_url 才生效；否则原样检索且 trace 标记 llm_failed。',
);

const RAG_RERANK_TOOLTIP = I18n.t(
  'workflow_knowledge_rag_rerank_required',
  {},
  '需 rag 部署侧配置 rerank_base_url 才生效；否则跳过 rerank 且不报错。',
);

export interface QueryEnhancementSectionProps {
  value: DataSetInfo;
  onChange: (next: DataSetInfo) => void;
  readonly?: boolean;
  disabled?: boolean;
}

export const QueryEnhancementSection: FC<QueryEnhancementSectionProps> = ({
  value,
  onChange,
  readonly,
  disabled,
}) => {
  const { getNodeSetterId } = useNodeTestId();

  return (
    <div>
      <TitleArea
        title={I18n.t('workflow_knowledge_query_enhancement', {}, '查询增强')}
      />
      <CheckboxWithLabel
        checked={!!value?.rewrite}
        onChange={checked => onChange({ ...value, rewrite: checked })}
        readonly={readonly || disabled}
        label={I18n.t('workflow_knowledge_rewrite', {}, '查询改写')}
        description={I18n.t(
          'workflow_knowledge_rewrite_desc',
          {},
          '改写为更适合检索的形式。',
        )}
        dataTestId={getNodeSetterId('dataset_rewrite')}
        tooltip={RAG_LLM_TOOLTIP}
        tipWrapperClassName={s['tips-container']}
      />
      <CheckboxWithLabel
        checked={!!value?.expansion}
        onChange={checked => onChange({ ...value, expansion: checked })}
        readonly={readonly || disabled}
        label={I18n.t('workflow_knowledge_expansion', {}, '查询扩展')}
        description={I18n.t(
          'workflow_knowledge_expansion_desc',
          {},
          '加入相关词。',
        )}
        dataTestId={getNodeSetterId('dataset_expansion')}
        tooltip={RAG_LLM_TOOLTIP}
        tipWrapperClassName={s['tips-container']}
      />
      <CheckboxWithLabel
        checked={!!value?.multi_query}
        onChange={checked => onChange({ ...value, multi_query: checked })}
        readonly={readonly || disabled}
        label={I18n.t('workflow_knowledge_multi_query', {}, '多重查询')}
        description={I18n.t(
          'workflow_knowledge_multi_query_desc',
          {},
          '生成多个语义等价查询并行检索。',
        )}
        dataTestId={getNodeSetterId('dataset_multi_query')}
        tooltip={RAG_LLM_TOOLTIP}
        tipWrapperClassName={s['tips-container']}
      />
      <CheckboxWithLabel
        checked={!!value?.enable_rerank}
        onChange={checked => onChange({ ...value, enable_rerank: checked })}
        readonly={readonly || disabled}
        label={I18n.t('workflow_knowledge_enable_rerank', {}, '启用 Rerank')}
        description={I18n.t(
          'workflow_knowledge_enable_rerank_desc',
          {},
          '使用 reranker 对召回结果重新排序。',
        )}
        dataTestId={getNodeSetterId('dataset_enable_rerank')}
        tooltip={RAG_RERANK_TOOLTIP}
        tipWrapperClassName={s['tips-container']}
      />
    </div>
  );
};
