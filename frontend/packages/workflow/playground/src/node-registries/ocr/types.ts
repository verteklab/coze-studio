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

export enum OCRProviderType {
  OpenAIVision = 'openai_vision',
  MinerU = 'mineru',
  PaddleOCR = 'paddleocr',
  Custom = 'custom',
}

export interface OpenAIVisionConfig {
  provider_id?: string;
  endpoint?: string;
  api_key?: string;
  model?: string;
  prompt?: string;
  max_tokens?: number;
  /** paddleocr | image_native | parts | pdf_data_url_string */
  message_format?: string;
  /** PaddleOCR wrapper: passed as paddleocr.output_format (default text) */
  paddleocr_output_format?: string;
}

export interface MinerUConfig {
  endpoint: string;
}

export interface PaddleOCRConfig {
  endpoint: string;
}

export interface CustomHTTPConfig {
  url: string;
  method?: string;
  content_type?: string;
  auth_type?: string;
  auth_token?: string;
  auth_header?: string;
  auth_value?: string;
  body_template?: string;
  headers?: Record<string, string>;
  json_path?: string;
}

export interface OCRNodeInputs {
  providerType: OCRProviderType;
  ocrConfig:
    | OpenAIVisionConfig
    | MinerUConfig
    | PaddleOCRConfig
    | CustomHTTPConfig;
  ocrSetting?: {
    timeout: number;
    retryTimes: number;
  };
}
