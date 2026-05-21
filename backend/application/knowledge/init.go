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

package knowledge

import (
	"context"
	"fmt"
	"os"

	"github.com/coze-dev/coze-studio/backend/application/search"
	ragconf "github.com/coze-dev/coze-studio/backend/conf/rag"
	knowledgeImpl "github.com/coze-dev/coze-studio/backend/domain/knowledge/service"
	"github.com/coze-dev/coze-studio/backend/domain/knowledge/service/ragimpl"
	"github.com/coze-dev/coze-studio/backend/infra/eventbus"
	infrarag "github.com/coze-dev/coze-studio/backend/infra/rag"
	"github.com/coze-dev/coze-studio/backend/types/consts"
)

type ServiceComponents = knowledgeImpl.KnowledgeSVCConfig

// ragConfigPath is the on-disk location of the rag config file. Overridable
// via RAG_CONFIG_PATH for tests / non-standard layouts; defaults to the
// repo's conventional location.
const ragConfigPath = "conf/rag/rag.yaml"

// InitService wires the KnowledgeApplicationService and selects the backend
// implementation based on the KNOWLEDGE_BACKEND env var:
//
//	legacy (default) -> coze's in-tree knowledge stack (NSQ consumer registered)
//	rag              -> ragimpl pointed at the external rag service (no NSQ consumer)
//
// Any other value is rejected at startup; we fail loudly rather than silently
// falling back, so misconfiguration is visible the moment the process boots.
func InitService(ctx context.Context, c *ServiceComponents, bus search.ResourceEventBus) (*KnowledgeApplicationService, error) {
	backend := os.Getenv(consts.KnowledgeBackendEnv)
	if backend == "" {
		backend = "legacy"
	}

	switch backend {
	case "legacy":
		return initLegacy(c, bus)
	case "rag":
		return initRag(ctx, c, bus)
	default:
		return nil, fmt.Errorf("unknown %s=%q (expected \"legacy\" or \"rag\")", consts.KnowledgeBackendEnv, backend)
	}
}

func initLegacy(c *ServiceComponents, bus search.ResourceEventBus) (*KnowledgeApplicationService, error) {
	knowledgeDomainSVC, knowledgeEventHandler := knowledgeImpl.NewKnowledgeSVC(c)

	nameServer := os.Getenv(consts.MQServer)
	if err := eventbus.GetDefaultSVC().RegisterConsumer(nameServer, consts.RMQTopicKnowledge, consts.RMQConsumeGroupKnowledge, knowledgeEventHandler); err != nil {
		return nil, fmt.Errorf("register knowledge consumer failed, err=%w", err)
	}

	KnowledgeSVC.DomainSVC = knowledgeDomainSVC
	KnowledgeSVC.eventBus = bus
	KnowledgeSVC.storage = c.Storage
	return KnowledgeSVC, nil
}

func initRag(ctx context.Context, c *ServiceComponents, bus search.ResourceEventBus) (*KnowledgeApplicationService, error) {
	cfgPath := os.Getenv("RAG_CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = ragConfigPath
	}
	cfg, err := ragconf.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("rag backend: load config %q: %w", cfgPath, err)
	}

	resolver, err := buildTenantResolver(cfg.Knowledge.Tenant)
	if err != nil {
		return nil, err
	}

	client := infrarag.New(cfg.Rag)
	if err := client.Ready(ctx); err != nil {
		return nil, fmt.Errorf("rag backend: upstream not ready at %s: %w", cfg.Rag.BaseURL, err)
	}

	domainSVC := ragimpl.New(
		client,
		c.DB,
		c.IDGen,
		resolver,
		c.Storage,
		cfg.Rag.DefaultTextEmbeddingModelID,
		cfg.Rag.DefaultImageEmbeddingModelID,
		cfg.Rag.DefaultLLMModelID,
		cfg.Rag.DefaultRerankModelID,
		cfg.Rag.DefaultOCRModelID,
	)

	// NB: NO NSQ consumer is registered for the rag backend. rag emits no
	// domain events into coze's bus — indexing progress is polled per-document
	// via GetTask instead.
	KnowledgeSVC.DomainSVC = domainSVC
	KnowledgeSVC.eventBus = bus
	KnowledgeSVC.storage = c.Storage
	KnowledgeSVC.rag = client
	KnowledgeSVC.ragTenantResolver = resolver
	// rag-only: tag outgoing Dataset DTOs with their owning backend so the
	// frontend can route upload flows accordingly. The DAO is independent of
	// the (private) MappingRepo embedded in ragimpl.Impl — application code
	// can't reach into Impl.mapping, so we hold our own handle here against
	// the same DB.
	KnowledgeSVC.mappingRepo = ragimpl.NewMappingRepo(c.DB)
	return KnowledgeSVC, nil
}

// buildTenantResolver maps a TenantConfig to a concrete resolver. Phase 1
// only supports mode=env; mode=user is rejected with a clear error so a
// future PR can flip it on without touching application wiring.
func buildTenantResolver(t ragconf.TenantConfig) (ragimpl.TenantResolver, error) {
	mode := t.Mode
	if mode == "" {
		mode = "env"
	}
	switch mode {
	case "env":
		return ragimpl.NewEnvTenantResolver(t.DefaultTenantID), nil
	case "user":
		return nil, fmt.Errorf("knowledge.tenant.mode=user not supported in this build")
	default:
		return nil, fmt.Errorf("knowledge.tenant.mode=%q not supported", mode)
	}
}
