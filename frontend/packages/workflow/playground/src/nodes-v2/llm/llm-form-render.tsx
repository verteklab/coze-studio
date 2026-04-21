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

import {
  Field,
  FieldArray,
  useService,
  type FieldRenderProps,
  type FieldArrayRenderProps,
  type FormRenderProps,
} from '@flowgram-adapter/free-layout-editor';
import { PublicScopeProvider } from '@coze-workflow/variable';
import { ViewVariableType } from '@coze-workflow/nodes';
import {
  concatTestId,
  type RefExpression,
  type ValueExpression,
  ValueExpressionType,
} from '@coze-workflow/base';
import { I18n } from '@coze-arch/i18n';
import { IconCozMinus, IconCozPlus } from '@coze-arch/coze-design/icons';
import { IconButton } from '@coze-arch/coze-design';

import { type IModelValue } from '@/typing';
import { WorkflowModelsService } from '@/services';
import { useReadonly } from '@/nodes-v2/hooks/use-readonly';
import { ValueExpressionInput } from '@/nodes-v2/components/value-expression-input';
import { Outputs } from '@/nodes-v2/components/outputs';
import { NodeInputName } from '@/nodes-v2/components/node-input-name';
import { FormItemFeedback } from '@/nodes-v2/components/form-item-feedback';
import { BatchMode } from '@/nodes-v2/components/batch-mode';
import { Batch } from '@/nodes-v2/components/batch/batch';
import { useGetWorkflowMode, useGlobalState } from '@/hooks';
import { FormCard } from '@/form-extensions/components/form-card';
import { ColumnsTitleWithAction } from '@/form-extensions/components/columns-title-with-action';
import { ModelSelect } from '@/components/model-select';

import { SettingOnError } from '../components/setting-on-error';
import NodeMeta from '../components/node-meta';
import { Vision, isVisionInput } from './vision';
import { llmNodeRenderDeps } from './utils/llm-form-render-deps';
import { UserPrompt } from './user-prompt';
import { type FormData } from './types';
import { SystemPrompt } from './system-prompt';
import { type BoundSkills } from './skills/types';
import { Skills } from './skills';
import { useResetCustomHTTPFields } from './hooks/use-reset-custom-http-fields';
import { isCustomHTTPModel } from './custom-http-utils';
import { sortOutputs } from './cot';
import { ChatHistory } from './chat-history';

import styles from './index.module.less';

export const Render = ({ form }: FormRenderProps<FormData>) => {
  const readonly = useReadonly();
  const { isChatflow } = useGetWorkflowMode();
  const { isBindDouyin } = useGlobalState();
  const modelsService = useService<WorkflowModelsService>(
    WorkflowModelsService,
  );
  const selectedModelType = form.getValueIn<number>('model.modelType');
  const selectedModel = modelsService.getModelByType(selectedModelType);
  const isCurrentCustomHTTPModel = isCustomHTTPModel(selectedModel);
  const resetCustomHTTPFields = useResetCustomHTTPFields(form, modelsService);

  return (
    <PublicScopeProvider>
      <>
        <NodeMeta
          deps={['outputs', 'batchMode']}
          outputsPath={'outputs'}
          batchModePath={'batchMode'}
        />
        <Field name={'batchMode'}>
          {({ field }: FieldRenderProps<string>) => (
            <BatchMode
              name={field.name}
              value={field.value}
              onChange={field.onChange}
              onBlur={field.onBlur}
            />
          )}
        </Field>
        <Field name={'model'}>
          {({ field }: FieldRenderProps<IModelValue | undefined>) => (
            <FormCard
              header={I18n.t('workflow_detail_llm_model')}
              tooltip={I18n.t('workflow_detail_llm_prompt_tooltip')}
            >
              <ModelSelect
                {...field}
                readonly={readonly}
                onChange={nextValue => {
                  field.onChange(nextValue);
                  resetCustomHTTPFields(nextValue);
                }}
              />
            </FormCard>
          )}
        </Field>
        <Batch batchModeName={'batchMode'} name={'batch'} />
        {!isBindDouyin && !isCurrentCustomHTTPModel ? (
          <Field name="fcParam">
            {({ field }: FieldRenderProps<BoundSkills | undefined>) => (
              <Skills {...field} />
            )}
          </Field>
        ) : null}
        <FieldArray
          name={'$$input_decorator$$.inputParameters'}
          defaultValue={[
            { name: 'input', input: { type: ValueExpressionType.REF } },
          ]}
        >
          {({
            field,
          }: FieldArrayRenderProps<{
            name: string;
            input: { type: ValueExpressionType };
          }>) => (
            <FormCard
              header={I18n.t('workflow_detail_node_parameter_input')}
              tooltip={I18n.t('workflow_detail_llm_input_tooltip')}
            >
              <div className={styles['columns-title']}>
                <ColumnsTitleWithAction
                  columns={llmNodeRenderDeps.columns}
                  readonly={readonly}
                />
              </div>
              {field.map((child, index) =>
                isVisionInput(child.value) ? null : (
                  <div
                    key={child.key}
                    style={{
                      display: 'flex',
                      alignItems: 'flex-start',
                      paddingBottom: 4,
                      gap: 4,
                    }}
                  >
                    <Field name={`${child.name}.name`}>
                      {({
                        field: childNameField,
                        fieldState: nameFieldState,
                      }: FieldRenderProps<string>) => (
                        <div
                          style={{
                            flex: 2,
                            minWidth: 0,
                          }}
                        >
                          <NodeInputName
                            {...childNameField}
                            input={form.getValueIn<RefExpression>(
                              `${child.name}.input`,
                            )}
                            inputParameters={field.value || []}
                            isError={!!nameFieldState?.errors?.length}
                          />
                          <FormItemFeedback errors={nameFieldState?.errors} />
                        </div>
                      )}
                    </Field>
                    <Field name={`${child.name}.input`}>
                      {({
                        field: childInputField,
                        fieldState: inputFieldState,
                      }: FieldRenderProps<ValueExpression | undefined>) => (
                        <div style={{ flex: 3, minWidth: 0 }}>
                          <ValueExpressionInput
                            {...childInputField}
                            isError={!!inputFieldState?.errors?.length}
                          />
                          <FormItemFeedback errors={inputFieldState?.errors} />
                        </div>
                      )}
                    </Field>
                    {readonly ? null : (
                      <div className="leading-none">
                        <IconButton
                          size="small"
                          color="secondary"
                          data-testid={concatTestId(child.name, 'remove')}
                          icon={<IconCozMinus className="text-sm" />}
                          onClick={() => {
                            field.delete(index);
                          }}
                        />
                      </div>
                    )}
                  </div>
                ),
              )}
              {isChatflow ? (
                <Field name={'$$input_decorator$$.chatHistorySetting'}>
                  {({
                    field: enableChatHistoryField,
                  }: FieldRenderProps<{
                    enableChatHistory: boolean;
                    chatHistoryRound: number;
                  }>) => (
                    <ChatHistory
                      {...enableChatHistoryField}
                      style={{ paddingRight: '32px' }}
                      showLine={false}
                    />
                  )}
                </Field>
              ) : null}
              {readonly ? null : (
                <div className={styles['input-add-icon']}>
                  <IconButton
                    className="!block"
                    color="highlight"
                    size="small"
                    icon={<IconCozPlus className="text-sm" />}
                    onClick={() => {
                      field.append({
                        name: '',
                        input: { type: ValueExpressionType.REF },
                      });
                    }}
                  />
                </div>
              )}
            </FormCard>
          )}
        </FieldArray>
        {!isBindDouyin && !isCurrentCustomHTTPModel ? <Vision /> : null}
        {!isCurrentCustomHTTPModel ? (
          <>
            <Field
              name="$$prompt_decorator$$.systemPrompt"
              deps={['$$input_decorator$$.inputParameters']}
              defaultValue={''}
            >
              {({ field }: FieldRenderProps<string>) => (
                <SystemPrompt
                  {...field}
                  placeholder={I18n.t('workflow_detail_llm_sys_prompt_content')}
                  fcParam={form.getValueIn('fcParam')}
                  inputParameters={form.getValueIn(
                    '$$input_decorator$$.inputParameters',
                  )}
                />
              )}
            </Field>
            <Field
              name="$$prompt_decorator$$.prompt"
              deps={['$$input_decorator$$.inputParameters', 'model']}
              defaultValue={''}
            >
              {({ field, fieldState }: FieldRenderProps<string>) => (
                <UserPrompt field={field} fieldState={fieldState} />
              )}
            </Field>
          </>
        ) : null}
        <Field
          name={'outputs'}
          deps={['batchMode']}
          defaultValue={[{ name: 'output', type: ViewVariableType.String }]}
        >
          {({ field, fieldState }) => (
            <Outputs
              id={'llm-node-output'}
              value={field.value}
              onChange={field.onChange}
              batchMode={form.getValueIn('batchMode')}
              withDescription
              showResponseFormat={!isCurrentCustomHTTPModel}
              titleTooltip={I18n.t('workflow_detail_llm_output_tooltip')}
              disabledTypes={[]}
              needErrorBody={form.getValueIn(
                'settingOnError.settingOnErrorIsOpen',
              )}
              errors={fieldState?.errors}
              sortValue={sortOutputs}
            />
          )}
        </Field>
        <SettingOnError outputsPath={'outputs'} batchModePath={'batchMode'} />
      </>
    </PublicScopeProvider>
  );
};
