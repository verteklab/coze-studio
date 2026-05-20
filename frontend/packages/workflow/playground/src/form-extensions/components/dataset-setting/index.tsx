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
import { type FC, useEffect, useState } from 'react';

import { isNil } from 'lodash-es';
import { type Dataset } from '@coze-arch/idl/knowledge';

import { Strategy, type DataSetInfo } from './type';
import {
  BasicSection,
  QueryEnhancementSection,
  QueryInputSection,
  FilterSection,
} from './sections';

import s from './index.module.less';

const DEFAULT_TOP_K = 10;

export interface DataSetSettingProps {
  dataSetInfo: DataSetInfo;
  onDataSetInfoChange: (v: DataSetInfo) => void;
  readonly?: boolean;
  disabled?: boolean;
  style?: Record<string, unknown>;
  isReady?: boolean;
  dataSets?: Dataset[];
}

export const DataSetSetting: FC<DataSetSettingProps> = ({
  dataSetInfo,
  onDataSetInfoChange,
  readonly,
  disabled,
  style,
  isReady,
  dataSets,
}) => {
  const [datasetEmpty, setDatasetEmpty] = useState(true);

  useEffect(() => {
    if (!isReady) {
      return;
    }
    setDatasetEmpty(!dataSets?.length);
    if (!dataSets?.length) {
      const empty: DataSetInfo = {};
      onDataSetInfoChange(empty);
    }
  }, [dataSets, isReady]);

  useEffect(() => {
    if (datasetEmpty) {
      return;
    }
    if (isNil(dataSetInfo?.top_k) && isNil(dataSetInfo?.strategy)) {
      onDataSetInfoChange({
        ...dataSetInfo,
        top_k: DEFAULT_TOP_K,
        strategy: Strategy.Hybird,
      });
    } else if (isNil(dataSetInfo?.strategy)) {
      onDataSetInfoChange({ ...dataSetInfo, strategy: Strategy.Hybird });
    }
  }, [dataSetInfo, datasetEmpty]);

  if (datasetEmpty) {
    return <></>;
  }

  return (
    <div className={s.setting} style={{ ...style, position: 'relative' }}>
      <BasicSection
        value={dataSetInfo}
        onChange={onDataSetInfoChange}
        readonly={readonly}
        disabled={disabled}
      />
      <QueryEnhancementSection
        value={dataSetInfo}
        onChange={onDataSetInfoChange}
        readonly={readonly}
        disabled={disabled}
      />
      <QueryInputSection
        value={dataSetInfo}
        onChange={onDataSetInfoChange}
        readonly={readonly}
        disabled={disabled}
      />
      <FilterSection
        value={dataSetInfo}
        onChange={onDataSetInfoChange}
        readonly={readonly}
        disabled={disabled}
      />
    </div>
  );
};
