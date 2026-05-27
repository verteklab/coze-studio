# R2-K: 把 `Retrieve.MaxTokens` 下沉到 rag，文档化"近似"裁剪语义

**Date:** 2026-05-15
**Status:** Draft
**Predecessor:** R2-F (2026-05-14)
**Sibling slices:** R2-H / R2-I / R2-J / R2-L / R2-F-Rerank / R2-G —— 同期可并行
**Companion gap:** `docs/rag-feature-gaps-zh.md` §A' 行 "检索 `MaxTokens`"

## 1. Motivation

`backend/domain/knowledge/service/ragimpl/retrieval.go:91-93` 当前注释把 `MaxTokens` 与 `MinScore` 同列为"coze-side post-filter"——但实际上 coze 这条 post-filter 是 **silent drop**（命中拼接超 token 预算的情况不会被预先裁掉），不是真过滤。用户在工作流里设了 MaxTokens，命中拼接长度仍可能超 LLM 上下文上限。

Rag 端 `RetrievalRequest` 顶层已接受 `max_tokens: int ≥ 1`（`rag/app/api/schemas/retrieval.py:37`）。**注意**：rag 的裁剪是"近似"的——按 chunk 边界停止累加，不是精确 token 计数（用户原话："retrieval approximate max_tokens"）。

R2-K 把字段接通；调用方若需要精确 token 预算，仍需自己在 service 层做二次精确 trim（本 slice 不做，是另一条独立讨论）。

## 2. Goals & non-goals

### Goals

- `backend/infra/contract/rag/types.go::RetrieveRequest` 加 `MaxTokens *int \`json:"max_tokens,omitempty"\``（与 R2-J 共改这个结构体，但字段独立）。
- `backend/domain/knowledge/service/ragimpl/retrieval.go` 写入 `req.Strategy.MaxTokens`。
- 在 `retrieval.go` 写入位置加一行**近似语义**注释：
  ```
  // MaxTokens is enforced by rag at chunk boundary granularity (approximate;
  // not exact token-count cutoff). Callers needing a strict budget should
  // post-process the returned slices.
  ```
- 删原 line 91-93 关于 MinScore/MaxTokens 的 stale 注释（R2-J 一并改）。
- Unit test：set / unset 两路径。
- httptest：lock body 含 `max_tokens`。

### Non-goals

- 在 ragimpl 内做精确 token 计数兜底裁剪。如果产品上需要"严格预算"，单独起 R2-K2（需引入 tokenizer，工作量大）。
- 改 `RetrieveStrategy.MaxTokens *int64` 的形状或调用约定。

## 3. Contract change

### 3.1 DTO 字段

```go
type RetrieveRequest struct {
    // ...
    MinScore  *float64  `json:"min_score,omitempty"`   // R2-J
    MaxTokens *int      `json:"max_tokens,omitempty"`  // NEW (R2-K)
    // ...
}
```

注意 service 层 `RetrieveStrategy.MaxTokens` 是 `*int64`；ragimpl 写入时需要类型转换：

```go
if req.Strategy.MaxTokens != nil {
    mt := int(*req.Strategy.MaxTokens)
    if mt < 1 {
        // rag schema 要求 ge=1；coze 在到达 rag 之前拒了更友好
        return nil, errorx.New(errno.ErrKnowledgeInvalidParamCode,
            errorx.KV("msg", "MaxTokens must be >= 1"))
    }
    ragReq.MaxTokens = &mt
}
```

### 3.2 文档化

在 `retrieval.go` 该段加一行说明 rag 的"近似"语义（见 Goals）。

也建议在 `docs/rag-feature-gaps-zh.md` §A' 表里保留 "approximate" 备注（已在 2026-05-15 校准时加上）。

## 4. Files touched

| File | Change |
|---|---|
| `backend/infra/contract/rag/types.go` | RetrieveRequest 加 `MaxTokens *int`（若 R2-J 先合，本 slice 只补这一行） |
| `backend/infra/rag/client_test.go` | httptest 锁形 |
| `backend/domain/knowledge/service/ragimpl/retrieval.go` | 写 MaxTokens + 加"近似"语义注释 + 删 stale 注释（与 R2-J 协调） |
| `backend/domain/knowledge/service/ragimpl/retrieval_test.go` | unit test：set / unset / <1 拒绝 |

## 5. Testing

Unit：
- `TestRetrieve_MaxTokens_Set` — `req.Strategy.MaxTokens = ptr(int64(2048))`，fake client 收到 `ragReq.MaxTokens == ptr(2048)`。
- `TestRetrieve_MaxTokens_Nil` — 未设 → 不传。
- `TestRetrieve_MaxTokens_Zero_Rejected` — `*MaxTokens == 0` → ErrKnowledgeInvalidParamCode，不调 rag。
- `TestRetrieve_MaxTokens_Negative_Rejected` — `*MaxTokens == -1` → 同上。

httptest：lock POST body 含 `"max_tokens": 2048`。

## 6. Compatibility & rollout

- DTO additive。
- 行为变化：以前 MaxTokens 字段被 silent drop，命中拼接可能超 LLM context；现在 rag 在 chunk 边界裁剪后返回。调用方如果以前依赖"reply 全量 TopK 再自己裁"，本 slice 改完后结果集会**变短**（rag 在 chunk 边界裁掉的那部分不会回来）。
- 这是 bug fix —— 旧行为是隐式 drop，新行为是 contract 兑现。
- 建议在 PR 描述里列出"行为差异"段，方便 reviewer 评估对工作流节点的影响。
