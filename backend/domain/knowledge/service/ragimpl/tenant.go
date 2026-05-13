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
	"errors"
)

// TenantResolver maps a request context to the rag tenant_id string that
// should be used for downstream rag calls.
//
// Phase 1: EnvTenantResolver returns a single configured value for all
// callers (one global tenant for the whole coze deployment).
//
// Phase 2 (future, not in this PR): UserTenantResolver reads
// `user.rag_tenant_id` from ctx-carried user info. The shape of the resolver
// interface is identical so swap-over is config-only.
type TenantResolver interface {
	Resolve(ctx context.Context) (string, error)
}

// EnvTenantResolver returns the configured tenant id for every call.
type EnvTenantResolver struct {
	tenantID string
}

func NewEnvTenantResolver(tenantID string) *EnvTenantResolver {
	return &EnvTenantResolver{tenantID: tenantID}
}

func (r *EnvTenantResolver) Resolve(_ context.Context) (string, error) {
	if r.tenantID == "" {
		return "", errors.New("ragimpl: env tenant_id is empty (set RAG_TENANT_ID)")
	}
	return r.tenantID, nil
}

// UserTenantResolver (Phase 2) — not wired up in PR-1. Adding this stub as
// commented code documents the contract; the user-info ctx key and the
// `user.rag_tenant_id` field need a separate PR to land before this can be
// activated.
//
// type UserTenantResolver struct {
//     userRepo user.Repository
// }
//
// func (r *UserTenantResolver) Resolve(ctx context.Context) (string, error) {
//     uid := ctxutil.UserIDFromCtx(ctx)
//     u, err := r.userRepo.Get(ctx, uid)
//     if err != nil { return "", err }
//     if u.RagTenantID == "" {
//         return "", errors.New("ragimpl: user has no rag_tenant_id assigned")
//     }
//     return u.RagTenantID, nil
// }
