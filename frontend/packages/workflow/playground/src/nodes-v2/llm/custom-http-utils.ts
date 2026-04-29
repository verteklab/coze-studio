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

import { type Model } from '@coze-arch/bot-api/developer_api';

export const isCustomHTTPModel = (model?: Model) => Boolean(model?.custom_http);

const MESSAGES_TOKEN = /\{\{\s*messages\s*\}\}/;

/**
 * A custom_http model is "chat-shaped" when its payload_template contains the
 * {{messages}} token. The coze-studio backend template engine fills this token
 * with workflow node's system_prompt + user_prompt + chat_history, so the
 * frontend must show the corresponding prompt input fields.
 *
 * Strict regex: does NOT match {{messages_count}}, {{messagesArray}}, etc.
 */
export const isChatShapedCustomHTTP = (model?: Model): boolean => {
  if (!isCustomHTTPModel(model)) return false;
  // The workflow Model exposes payload_template directly on custom_http
  // (mirrored by application/modelmgr from model_instance.connection in coze
  // backend). The admin Model nests it under connection — these are
  // different IDL types; do not collapse them.
  const template = model?.custom_http?.payload_template ?? '';
  return MESSAGES_TOKEN.test(template);
};

/**
 * True when the LLM-style fields (system/user prompt, skills, vision, response
 * format) should be rendered for this model. Built-in models always show these;
 * custom_http only shows them when chat-shaped.
 */
export const showsLLMFields = (model?: Model): boolean =>
  !isCustomHTTPModel(model) || isChatShapedCustomHTTP(model);
