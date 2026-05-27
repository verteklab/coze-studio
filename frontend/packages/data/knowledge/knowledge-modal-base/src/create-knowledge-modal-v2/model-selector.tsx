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

import React, { useEffect, useState } from 'react';

import { Select } from '@coze-arch/coze-design';

export interface ModelProvider {
  id: string;
  name: string;
}

export interface ProvidersResponse {
  text_models: ModelProvider[];
  image_models: ModelProvider[];
}

export interface ModelSelectorProps {
  fetchProviders: () => Promise<ProvidersResponse>;
  onChange: (sel: { textModelId: string; imageModelId: string }) => void;
}

export const ModelSelector: React.FC<ModelSelectorProps> = ({
  fetchProviders,
  onChange,
}) => {
  const [providers, setProviders] = useState<ProvidersResponse | null>(null);
  const [text, setText] = useState<string>('');
  const [image, setImage] = useState<string>('');

  useEffect(() => {
    fetchProviders().then(p => {
      setProviders(p);
      const defText = p.text_models[0]?.id ?? '';
      const defImage = p.image_models[0]?.id ?? '';
      setText(defText);
      setImage(defImage);
      onChange({ textModelId: defText, imageModelId: defImage });
    });
  }, [fetchProviders, onChange]);

  if (!providers) {
    return <div>Loading models…</div>;
  }

  return (
    <div className="space-y-2">
      <Select
        value={text}
        onChange={v => {
          const next = String(v ?? '');
          setText(next);
          onChange({ textModelId: next, imageModelId: image });
        }}
        optionList={providers.text_models.map(m => ({
          label: m.name,
          value: m.id,
        }))}
      />
      <Select
        value={image}
        onChange={v => {
          const next = String(v ?? '');
          setImage(next);
          onChange({ textModelId: text, imageModelId: next });
        }}
        optionList={providers.image_models.map(m => ({
          label: m.name,
          value: m.id,
        }))}
      />
    </div>
  );
};
