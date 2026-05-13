/*
 * Copyright 2026 coze-dev Authors
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

package consts

import "context"

// RagModelOverrideKey is the context key carrying explicit embedding model
// ids from the create-KB request to ragimpl. We use a zero-sized struct
// type as the key so the value type is unforgeable from outside this
// package (cannot collide with strings or other consumers' keys).
type RagModelOverrideKey struct{}

// RagModelOverride carries optional per-request embedding model selections
// that the application layer (CreateDataset handler) wants to pass into
// ragimpl.CreateKnowledge. Either field may be empty; ragimpl falls back to
// its configured defaults for empty values.
type RagModelOverride struct {
	TextModelID  string
	ImageModelID string
}

// WithRagModelOverride attaches override values to ctx. If both fields are
// empty the context is returned unchanged — we never want a no-op override
// to mask a future caller's real override.
func WithRagModelOverride(ctx context.Context, text, image string) context.Context {
	if text == "" && image == "" {
		return ctx
	}
	return context.WithValue(ctx, RagModelOverrideKey{}, RagModelOverride{
		TextModelID:  text,
		ImageModelID: image,
	})
}

// RagModelOverrideFromContext returns the override attached to ctx (if any).
// The second return is false when no override was attached.
func RagModelOverrideFromContext(ctx context.Context) (RagModelOverride, bool) {
	v, ok := ctx.Value(RagModelOverrideKey{}).(RagModelOverride)
	return v, ok
}
