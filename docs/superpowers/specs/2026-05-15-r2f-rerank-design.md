# R2-F-Rerank: wire `EnableRerank` to rag 的 `query_strategy.{enable_rerank, rerank_model_id}`

**Date:** 2026-05-15
**Status:** Draft（解锁：rag 端 rerank 能力已注册，see CLAUDE.md non-negotiable #11 / 用户 2026-05-15 确认）
**Predecessor:** R2-F (2026-05-14) —— 完全同构的 sibling slice
**Sibling slices:** R2-G / R2-H / R2-I / R2-J / R2-K / R2-L —— 同期可并行
**Companion gap:** `docs/rag-feature-gaps-zh.md` §A' 行 "检索 rerank"

## 1. Motivation

`backend/domain/knowledge/service/ragimpl/retrieval.go:117-119` 当前注释：

```go
// EnableRerank is exposed through fusion_policy or retriever_params on
// rag; the precise mapping is pending (see rag/docs §10.5). Leaving
// the field un-translated keeps us forward-compatible.
```

—— `req.Strategy.EnableRerank` 被静默 drop。检索质量没有 rerank 阶段加持。

Rag 端 rerank 模型已注册（per CLAUDE.md / 用户确认），`query_strategy` 接受 `enable_rerank` + `rerank_model_id`（plan-time 用 curl 或读 `rag/app/policy/validators/retrieval_validator.py` 确认精确字段名 —— 如果实际是放在 `fusion_policy` 下，本 spec 的 §3 需相应调整）。R2-F-Rerank 用与 R2-F 完全同构的方式接通：env-driven 默认 model id + 翻译。

## 2. Goals & non-goals

### Goals

- `backend/conf/rag/config.go` 加 `DefaultRerankModelID string` 字段，从 `${RAG_DEFAULT_RERANK_MODEL_ID}` 在 `rag.yaml` 读取。
- `ragimpl.Impl` 加 `defaultRerankModelID string`，构造时透传（`application/knowledge/init.go` 加一参）。
- `ragimpl.Retrieve` 在 `req.Strategy.EnableRerank` 为 true 且 `i.defaultRerankModelID != ""` 时，在 `ragReq.QueryStrategy` 里追加：
  ```
  "enable_rerank": true,
  "rerank_model_id": i.defaultRerankModelID
  ```
  注意 `QueryStrategy` 可能已被 R2-F 的 query rewrite 路径设过 —— 用 map merge 而非覆盖。
- 当 `EnableRerank=true` 但 env 空 → drop + WARN（与 R2-F 的 query rewrite 路径行为对称）。
- Unit test：env set / env empty 两路径 + 与 query rewrite 共存（QueryStrategy 同时含两组键）。
- `.env.example` / `docker/.env.debug` 加 `RAG_DEFAULT_RERANK_MODEL_ID=` 行。

### Non-goals

- per-call rerank model id override from UI。R2-D-fe-Wizard 或独立 slice。
- 在 rag 注册新 rerank 模型（已就绪，本 slice 不动 rag 端 config）。

## 3. Contract change

### 3.1 New config field

`backend/conf/rag/config.go`：

```go
type RagConfig struct {
    // ...
    DefaultLLMModelID    string `yaml:"default_llm_model_id"`     // R2-F
    DefaultRerankModelID string `yaml:"default_rerank_model_id"`  // NEW
    // ...
}
```

`backend/conf/rag/rag.yaml`：

```yaml
rag:
  # ...
  default_llm_model_id: "${RAG_DEFAULT_LLM_MODEL_ID}"
  # Rerank model id used when retrieval requests EnableRerank=true.
  # When empty, EnableRerank is silently dropped with a WARN log.
  default_rerank_model_id: "${RAG_DEFAULT_RERANK_MODEL_ID}"
```

### 3.2 ragimpl.Impl 扩展

加字段 + `New(...)` 多一个 string 参数（追加在 `defaultLLMModelID` 之后，保持顺序与三类 env 一致）：

```go
func New(
    rag contract.Client,
    db *gorm.DB,
    idgen idgen.IDGenerator,
    resolver TenantResolver,
    storage storage.Storage,
    defaultTextModel, defaultImageModel, defaultLLMModel, defaultRerankModel string,
) *Impl
```

### 3.3 Retrieval 翻译

`retrieval.go` 当前 line 117-119 的注释那段替换为：

```go
if req.Strategy.EnableRerank {
    if i.defaultRerankModelID != "" {
        if ragReq.QueryStrategy == nil {
            ragReq.QueryStrategy = map[string]any{}
        }
        ragReq.QueryStrategy["enable_rerank"] = true
        ragReq.QueryStrategy["rerank_model_id"] = i.defaultRerankModelID
    } else {
        logs.CtxWarnf(ctx, "ragimpl.Retrieve: EnableRerank=true but RAG_DEFAULT_RERANK_MODEL_ID is empty; dropping rerank to avoid rag 40004")
    }
}
```

注意与 R2-F 的 query rewrite 块共存：两者都可能往 `QueryStrategy` 里写键，必须用 map merge 而非整体赋值。

### 3.4 字段名 / 路径确认（plan-time）

本 spec 假设 rag 接受 `query_strategy.{enable_rerank, rerank_model_id}`。如果实际是 `fusion_policy.rerank_model_id` 或其它：
- 改 §3.3 写入路径
- 改 unit test 的断言对象（`ragReq.FusionPolicy` 而非 `ragReq.QueryStrategy`）

Plan 第一步：curl `/api/v1/retrieval` 触发 `enable_rerank=true, rerank_model_id="x"` 看 422 错误信息提示字段名，或读 `rag/app/policy/validators/retrieval_validator.py` 直接找。

## 4. Files touched

| File | Change |
|---|---|
| `backend/conf/rag/config.go` | 加 `DefaultRerankModelID` |
| `backend/conf/rag/rag.yaml` | 加 env 行 + 注释 |
| `backend/domain/knowledge/service/ragimpl/factory.go` | Impl 加字段 + `New` 签名扩展 |
| `backend/domain/knowledge/service/ragimpl/retrieval.go` | 替换 line 117-119 注释为真实翻译 |
| `backend/application/knowledge/init.go` | `ragimpl.New(...)` 加入 `cfg.Rag.DefaultRerankModelID` |
| `backend/domain/knowledge/service/ragimpl/retrieval_test.go` | unit test：set / empty / 与 rewrite 共存 |
| `backend/domain/knowledge/service/ragimpl/knowledge_test.go` | `newTestImpl` 多传一个空串 |
| `backend/domain/knowledge/service/ragimpl/integration_test.go` | 更新 `New(...)` 调用 |
| `docker/.env.debug` (template) | 加 `export RAG_DEFAULT_RERANK_MODEL_ID="..."` |

## 5. Testing

Unit：
- `TestRetrieve_EnableRerank_WithModelID` — 设 `defaultRerankModelID = "model-rerank-x"`，`EnableRerank: true` → `ragReq.QueryStrategy["enable_rerank"] == true && ["rerank_model_id"] == "model-rerank-x"`。
- `TestRetrieve_EnableRerank_NoModelID_Drops` — `defaultRerankModelID = ""`，`EnableRerank: true` → `QueryStrategy` 不含 rerank 键（但若 R2-F 的 rewrite 设了 `llm_model_id`，那两个键仍在）。
- `TestRetrieve_EnableRerank_WithRewrite_Coexist` — 两个 env 都设 + 两个 Enable 都开 → QueryStrategy 四个键齐全。

httptest：lock POST body 的 `query_strategy` 字段含 `enable_rerank` + `rerank_model_id`。

## 6. Failure modes

| Scenario | Behavior |
|---|---|
| Env 设了有效 rerank model id | rerank 启用 → rag 40x 不再 |
| Env 设了无效 id（typo） | rag 4xx "model not found"；R2-C decoder surfacing；用户改 env |
| Env 空，EnableRerank=true | drop + WARN，普通检索仍走 |
| Env 空，EnableRerank=false | 无变化 |
| Env 指向非 rerank 类型的模型 | rag 4xx；同 typo 处理 |

## 7. Compatibility & rollout

完全 additive。无 DB / IDL / 前端变化。`ragimpl.New` 签名变了但属内部 API（与 R2-F 同模式）。
