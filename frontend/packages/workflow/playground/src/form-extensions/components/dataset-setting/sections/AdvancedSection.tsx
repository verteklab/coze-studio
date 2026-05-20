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
import { type FC, useEffect, useRef, useState } from 'react';

import { I18n } from '@coze-arch/i18n';
import { Checkbox, TextArea, Collapse } from '@coze-arch/coze-design';
import { IconWarningInfo } from '@coze-arch/bot-icons';

import { type DataSetInfo } from '../type';
import s from '../index.module.less';
import { TitleArea } from '../components';

type Retriever = 'dense' | 'bm25' | 'image_vector';

const RETRIEVERS: Retriever[] = ['dense', 'bm25', 'image_vector'];

export interface AdvancedSectionProps {
  value: DataSetInfo;
  onChange: (next: DataSetInfo) => void;
  readonly?: boolean;
  disabled?: boolean;
}

const stringifyJSON = (v: unknown): string => {
  if (v === undefined || v === null) {
    return '';
  }
  try {
    return JSON.stringify(v, null, 2);
  } catch {
    return '';
  }
};

const parseJSONLoose = (
  raw: string,
): { ok: boolean; value: Record<string, unknown> } => {
  const trimmed = raw.trim();
  if (!trimmed) {
    return { ok: true, value: {} };
  }
  try {
    const parsed = JSON.parse(trimmed);
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return { ok: true, value: parsed as Record<string, unknown> };
    }
    return { ok: false, value: {} };
  } catch {
    return { ok: false, value: {} };
  }
};

export const AdvancedSection: FC<AdvancedSectionProps> = ({
  value,
  onChange,
  readonly,
  disabled,
}) => {
  const [open, setOpen] = useState(false);
  const retrievers = (value?.retrievers ?? []) as Retriever[];

  const [fpText, setFpText] = useState(stringifyJSON(value?.fusion_policy));
  const [fpError, setFpError] = useState<string | null>(null);
  const [rpText, setRpText] = useState(stringifyJSON(value?.retriever_params));
  const [rpError, setRpError] = useState<string | null>(null);

  const fpFocusedRef = useRef(false);
  const rpFocusedRef = useRef(false);

  useEffect(() => {
    if (!fpFocusedRef.current) {
      setFpText(stringifyJSON(value?.fusion_policy));
    }
  }, [value?.fusion_policy]);

  useEffect(() => {
    if (!rpFocusedRef.current) {
      setRpText(stringifyJSON(value?.retriever_params));
    }
  }, [value?.retriever_params]);

  return (
    <Collapse
      activeKey={open ? 'adv' : undefined}
      onChange={k =>
        setOpen(Array.isArray(k) ? k.includes('adv') : k === 'adv')
      }
    >
      <Collapse.Panel
        itemKey="adv"
        header={
          <span className="inline-flex items-center gap-1">
            <IconWarningInfo />
            {I18n.t(
              'workflow_knowledge_advanced',
              {},
              '高级（需 RAG 调参经验）',
            )}
          </span>
        }
      >
        <div className={s['setting-item']}>
          <TitleArea
            title={I18n.t('workflow_knowledge_retrievers', {}, 'Retrievers')}
            tip={I18n.t(
              'workflow_knowledge_retrievers_tip',
              {},
              '显式指定召回路：dense / bm25 / image_vector。留空由 target_chunk_types 推导。',
            )}
          />
          <div className="flex gap-3">
            {RETRIEVERS.map(r => (
              <Checkbox
                key={r}
                disabled={readonly || disabled}
                checked={retrievers.includes(r)}
                onChange={e => {
                  const set = new Set(retrievers);
                  if (e.target.checked) {
                    set.add(r);
                  } else {
                    set.delete(r);
                  }
                  onChange({ ...value, retrievers: Array.from(set) });
                }}
              >
                {r}
              </Checkbox>
            ))}
          </div>
        </div>

        <div className={s['setting-item']}>
          <TitleArea
            title={I18n.t(
              'workflow_knowledge_fusion_policy',
              {},
              'Fusion policy (JSON)',
            )}
            tip={I18n.t(
              'workflow_knowledge_fusion_policy_tip',
              {},
              '{"rrf_k":60, "weights":{"text":0.6, "image":0.4}}',
            )}
          />
          <TextArea
            disabled={readonly || disabled}
            value={fpText}
            rows={4}
            onChange={v => setFpText(v)}
            onFocus={() => {
              fpFocusedRef.current = true;
            }}
            onBlur={() => {
              fpFocusedRef.current = false;
              const r = parseJSONLoose(fpText);
              if (!r.ok) {
                setFpError(I18n.t('workflow_json_invalid', {}, 'JSON 无效'));
                return;
              }
              setFpError(null);
              onChange({ ...value, fusion_policy: r.value });
            }}
          />
          {fpError ? (
            <div className="text-red-500 text-xs">{fpError}</div>
          ) : null}
        </div>

        <div className={s['setting-item']}>
          <TitleArea
            title={I18n.t(
              'workflow_knowledge_retriever_params',
              {},
              'Retriever params (JSON)',
            )}
            tip={I18n.t(
              'workflow_knowledge_retriever_params_tip',
              {},
              '{"dense":{"candidate_k":75}, "bm25":{"candidate_k":40}}',
            )}
          />
          <TextArea
            disabled={readonly || disabled}
            value={rpText}
            rows={4}
            onChange={v => setRpText(v)}
            onFocus={() => {
              rpFocusedRef.current = true;
            }}
            onBlur={() => {
              rpFocusedRef.current = false;
              const r = parseJSONLoose(rpText);
              if (!r.ok) {
                setRpError(I18n.t('workflow_json_invalid', {}, 'JSON 无效'));
                return;
              }
              setRpError(null);
              onChange({ ...value, retriever_params: r.value });
            }}
          />
          {rpError ? (
            <div className="text-red-500 text-xs">{rpError}</div>
          ) : null}
        </div>
      </Collapse.Panel>
    </Collapse>
  );
};
