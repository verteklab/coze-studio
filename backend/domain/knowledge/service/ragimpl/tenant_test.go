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

package ragimpl

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvTenantResolver_ReturnsConfiguredValue(t *testing.T) {
	r := NewEnvTenantResolver("coze")
	got, err := r.Resolve(context.Background())
	require.NoError(t, err)
	require.Equal(t, "coze", got)
}

func TestEnvTenantResolver_IgnoresContextUser(t *testing.T) {
	r := NewEnvTenantResolver("coze")
	// Even if ctx carries a user with a different tenant value,
	// the env resolver should ignore it.
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("user_rag_tenant_id"), "other")
	got, err := r.Resolve(ctx)
	require.NoError(t, err)
	require.Equal(t, "coze", got)
}

func TestEnvTenantResolver_RejectsEmptyDefault(t *testing.T) {
	r := NewEnvTenantResolver("")
	_, err := r.Resolve(context.Background())
	require.Error(t, err)
}
