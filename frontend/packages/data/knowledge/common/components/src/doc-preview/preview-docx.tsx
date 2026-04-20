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

import { useEffect, useState } from 'react';

import DOMPurify from 'dompurify';
import { Spin } from '@coze-arch/coze-design';

interface IPreviewDocxProps {
  fileUrl: string;
}

// Lazily load mammoth so the ~500KB bundle is only paid when previewing a docx.
async function convertDocxToHtml(buffer: ArrayBuffer): Promise<string> {
  const mammoth = await import('mammoth/mammoth.browser');
  const { value } = await mammoth.convertToHtml({ arrayBuffer: buffer });
  return value;
}

export const PreviewDocx = (props: IPreviewDocxProps) => {
  const { fileUrl } = props;
  const [html, setHtml] = useState<string>('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string>('');

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError('');
    setHtml('');

    fetch(fileUrl)
      .then(res => {
        if (!res.ok) {
          throw new Error(`Failed to fetch document: ${res.status}`);
        }
        return res.arrayBuffer();
      })
      .then(convertDocxToHtml)
      .then(rawHtml => {
        if (cancelled) {
          return;
        }
        setHtml(DOMPurify.sanitize(rawHtml));
      })
      .catch((e: unknown) => {
        if (cancelled) {
          return;
        }
        setError(e instanceof Error ? e.message : String(e));
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [fileUrl]);

  return (
    <div className="flex flex-col items-center w-full h-full flex-1 py-2 px-4 overflow-auto">
      <Spin
        wrapperClassName="w-full h-full grow"
        spinning={loading}
        childStyle={{
          width: '100%',
          height: '100%',
          flexGrow: 1,
        }}
      >
        {error ? (
          <div className="coz-fg-hglt-red text-[14px] leading-[22px] p-4">
            {error}
          </div>
        ) : (
          <div
            className="docx-preview max-w-full text-[14px] leading-[22px] break-words"
            dangerouslySetInnerHTML={{ __html: html }}
          />
        )}
      </Spin>
    </div>
  );
};
