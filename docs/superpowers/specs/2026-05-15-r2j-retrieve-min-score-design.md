# R2-J: 把 `Retrieve.MinScore` 下沉到 rag，移除 coze 端 post-filter

**Date:** 2026-05-15
**Status:** Draft
**Predecessor:** R2-F (2026-05-14)
**Sibling slices:** R2-H / R2-I / R2-K / R2-L / R2-F-Rerank / R2-G —— 同期可并行
**Companion gap:** `docs/rag-feature-gaps-zh.md` §A' 行 "检索 `MinScore`"

## 1. Motivation

`backend/domain/knowledge/service/ragimpl/retrieval.go:91-93` 的注释：

```go
// MinScore / MaxTokens are coze-side post-filter knobs; rag does not
// accept them on /retrieval. We leave them un-applied here; the
// service-layer caller already trims by MinScore after the call.
```

调用方拿到全量 TopK 才裁剪——浪费带宽，且 rag 端如果有更聪明的 fusion/rerank 后裁剪逻辑也用不上。

Rag 端 `RetrievalRequest` 顶层已接受 `min_score: float ≥ 0`（`rag/app/api/schemas/retrieval.py:36`）。R2-J 把它接通，**同步移除 coze service 层 post-filter**（位置见 §3.2），让 rag 成为唯一的过滤点。

## 2. Goals & non-goals

### Goals

- `backend/infra/contract/rag/types.go::RetrieveRequest` 加 `MinScore *float64 \`json:"min_score,omitempty"\``。
- `backend/domain/knowledge/service/ragimpl/retrieval.go` 在拼装 `ragReq` 时，若 `req.Strategy.MinScore != nil`，写入 `ragReq.MinScore`。删原 line 91-93 stale 注释。
- **同步删除** coze service 层（或调用方 application 层）现有的 `MinScore` post-filter 代码。plan-time 用 grep 定位（关键词："MinScore" 或 "min_score" 的过滤 / slice trim）；通常在 `application/knowledge/` 或 `domain/knowledge/service/impl/` 里。
- Unit test：`MinScore` 设了 → ragReq 带值；未设 → 不传。
- httptest：lock body 含 `min_score`。
- post-filter 删除点附近的单元/集成 test 同步调整。

### Non-goals

- 改变 `RetrieveStrategy.MinScore *float64` 的类型与语义。
- 在 ragimpl 内做"以防万一"的二次裁剪。rag 是唯一权威。

## 3. Contract change

### 3.1 DTO 字段

```go
type RetrieveRequest struct {
    // ...
    MinScore  *float64  `json:"min_score,omitempty"`   // NEW
    MaxTokens *int      `json:"max_tokens,omitempty"`  // R2-K 一并加（或两 slice 独立加，看合并顺序）
    // ...
}
```

注：R2-K 也加 `MaxTokens` 字段。若 R2-J 先合，则只加 MinScore；R2-K 再合时补 MaxTokens。两 slice 顺序无依赖。

### 3.2 ragimpl 写入

`retrieval.go` 现有 `req.Strategy` 块内：

```go
if req.Strategy.MinScore != nil {
    ms := *req.Strategy.MinScore
    ragReq.MinScore = &ms
}
```

删除现有 line 91-93 注释。

### 3.3 移除 coze 端 post-filter

plan-time 必做：
1. `grep -rn "MinScore" backend/application/knowledge backend/domain/knowledge/service` 找到现有 trim 点
2. 删除该处的过滤循环 / slice trim
3. 调整对应单元 test：以前是 "service 层 trim 验证"，改为 "确认结果未被本地 trim，原样返回 rag 数据"

## 4. Files touched

| File | Change |
|---|---|
| `backend/infra/contract/rag/types.go` | RetrieveRequest 加 `MinScore *float64` |
| `backend/infra/rag/client_test.go` | httptest 锁形 |
| `backend/domain/knowledge/service/ragimpl/retrieval.go` | 写 MinScore + 删注释 |
| `backend/domain/knowledge/service/ragimpl/retrieval_test.go` | unit test：set / unset |
| (plan-time 定位) `application/knowledge/...` 或 `domain/knowledge/service/...` | 删 post-filter + 调相关 test |

## 5. Testing

Unit：
- `TestRetrieve_MinScore_Set` — `req.Strategy.MinScore = ptr(0.7)`，fake client 收到 `ragReq.MinScore == ptr(0.7)`。
- `TestRetrieve_MinScore_Nil` — 未设 → fake client 收到 `ragReq.MinScore == nil`。

httptest：lock POST body 含 `"min_score": 0.7`。

post-filter 删除点：原本依赖 service 层 trim 的 test 改成"验证返回数据等同于 rag 输入"。

## 6. Compatibility & rollout

- DTO 层 additive。
- ragimpl 行为变化：以前 rag 拿不到 MinScore → 返回 TopK 全量 → coze 后置过滤；现在 rag 直接过滤 → 返回 ≤ TopK 条命中。**调用方收到的结果集语义不变**（仍然 ≥ MinScore），只是分数边界处的"恰好相等"case 由 rag 决定（包含 vs 排除取决于 rag 实现，pydantic schema 是 `ge=0`，但实际比较 `>=` 还是 `>` 需 plan-time 在 rag 代码里确认）。
- 若 rag 的边界语义与 coze 旧 post-filter 不一致（`>` vs `>=`），考虑：
  - (a) 接受新语义（rag 是唯一权威，符合本 slice 目标）
  - (b) coze 在 ragimpl 内额外做一层"边界包含"的 trim —— 等同于不彻底移除 post-filter

  推荐 (a)，并在 changelog 里点明。
