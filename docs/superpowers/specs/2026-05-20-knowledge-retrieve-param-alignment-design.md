# 2026-05-20 — knowledge-retrieve 节点参数与 rag 接口对齐设计

## 背景

coze workflow 的 knowledge-retrieve 节点向 rag `POST /api/v1/retrieval`
发送的请求载荷，与 rag 当前 schema 在多个字段上不对齐：

- coze 发送 `document_ids / min_score / max_tokens` —— rag schema 不定义
  这些 key，Pydantic `extra="ignore"` 直接吞掉，用户 UI 上设置后**毫无
  效果**。
- coze 发送 `query_strategy.llm_model_id / query_strategy.rerank_model_id`
  —— rag validator 现在会因为不在 `SUPPORTED_QUERY_STRATEGIES` 直接返
  40004。**只有在 `RAG_DEFAULT_LLM_MODEL_ID` env 没设置时 coze 不送，所
  以现在能跑**；env 一旦配上，反而触发检索失败。
- coze 顶层发送 `candidate_k`，rag 的 policy resolver 不读，仅落 debug
  snapshot；仍是 silent no-op。
- rag schema 支持但 coze 节点从未暴露的能力：`query_image / query_mode /
  target_chunk_types / filters / retrievers / fusion_policy /
  retriever_params / query_strategy.{expansion, multi_query}`。
- coze 节点 UI 上还存留 `use_nl2sql / is_personal_only` 两个开关，前者
  会触发 `ErrRagFeaturePending 501`，后者是 coze-local KB 时代的本地过
  滤，rag 接管后语义不再适用。

> 详细对照见 user memory `coze-rag-retrieval-param-mismatch.md`。

本设计的目标：**让 coze 节点的请求载荷与 rag 真实 schema 一一对应**——
UI 只暴露 rag 真正读取的字段，并把 rag 已支持但未暴露的能力补到节点
表单上。

## 范围

- **改动只发生在 coze 侧**。rag 不动。
- 范围内：workflow knowledge-retrieve 节点的前端表单 + 后端 Adapt /
  RetrievalStrategy / ragimpl 转发链路 + service 层老字段彻底清理。
- 范围外：rag schema 修改；coze-local 知识库（已被 rag 接管）；图片
  embedding pipeline；KB 管理页的设置；agent / bot 侧的检索调用（如有，
  独立改动）。

### legacy 后端与 agent 路径的连带影响

`RetrievalStrategy` 是公共结构体，同时被 `KNOWLEDGE_BACKEND=legacy`
路径（`domain/knowledge/service/retrieve.go` 里的 `knowledgeSVC`）和
`agent/singleagent/internal/agentflow/node_retriever.go` 引用。删除
`EnableQueryRewrite / EnableRerank / EnableNL2SQL / IsPersonalOnly /
MinScore / MaxTokens` 6 个字段会同时影响这两条非 rag 路径：

- legacy `queryRewriteNode / nl2SqlRetrieveNode / packResults` 里依赖
  这些字段的代码同步移除（注意：移除会让 legacy 后端用户失去 query
  rewrite + NL2SQL + IsPersonalOnly + MinScore + MaxTokens 这些能力）
- `agentflow/node_retriever.go` 里 set 这些字段的代码删除

release note 需要明确写：legacy / agent 路径上这几个开关不再生效。改动
是 contract 一致性优先于 legacy 行为保持，符合 "knowledge-base
replacement" 主轴。

## 字段映射表

| 节点字段 | rag 字段 | 状态 |
|---|---|---|
| Query | `query` | KEEP |
| datasetParam (kb_ids) | `kb_ids` | KEEP |
| topK（**默认值改 5 → 10**，与 rag `retrieval_default_top_k` 一致） | `top_k` | KEEP |
| strategy（dense / hybrid / fulltext-bm25） | `search_type` | KEEP |
| queryImage + queryMode | `query_image` + `query_mode` | NEW |
| targetChunkTypes | `target_chunk_types` | NEW |
| filters | `filters` | NEW |
| 查询增强组 4 boolean | `query_strategy.{rewrite, expansion, multi_query, enable_rerank}` | KEEP+ADD |
| retrievers | `retrievers` | NEW（高级折叠） |
| fusionPolicy | `fusion_policy` | NEW（高级折叠） |
| retrieverParams | `retriever_params`（含 per-retriever candidate_k） | NEW（高级折叠） |
| ~~min_score~~ | — | DROP |
| ~~document_ids~~ | — | DROP |
| ~~use_nl2sql~~ | — | DROP |
| ~~is_personal_only~~ | — | DROP |
| ~~max_tokens~~ | — | DROP |
| ~~candidate_k (top-level)~~ | — | DROP（rag 不读；per-retriever candidate_k 改走 retriever_params） |
| ~~query_strategy.llm_model_id~~ | — | DROP（rag 会 40004） |
| ~~query_strategy.rerank_model_id~~ | — | DROP（rag 会 40004） |

### 默认值

| 字段 | UI 默认 | 上线发什么 | rag 端最终值 |
|---|---|---|---|
| `top_k` | **10**（slider mark 改 5→10） | 10 | 直接采纳；省略时 `kb.default_top_k` → settings `retrieval_default_top_k = 10`；上限 50 |
| `search_type` | Hybrid | `"hybrid"` | 直接采纳 |
| `query_mode` | `text_input` | 显式发 `"text_input"`（现有行为） | 与 query/query_image 一致性校验 |
| `target_chunk_types` | 空 | omit | text 查询 = `[text_chunk]`；image 查询 = `[image_chunk]`；混合 = 两者 ∩ `kb.enabled_chunk_types` |
| `retrievers` | 空 | omit | text → dense + bm25；image → image_vector；∩ `kb.enabled_retrievers` |
| `filters` | 空 `{}` | omit | `{}` |
| `fusion_policy` | 空 `{}` | omit | `kb.default_fusion_policy` → 系统默认 `text_weight=0.6, image_weight=0.4, rrf_k=60` |
| `retriever_params` | 空 `{}` | omit | per-retriever 系统默认：dense `candidate_k=50`，bm25 `candidate_k=50`，image_vector `candidate_k=30`；上限 200 |
| `query_strategy.*` 4 boolean | 全 false | 全 false 时 omit 整个 dict | false |
| `query_image` | none | omit | n/a |

## 前端设计

### 组件结构

`frontend/packages/workflow/playground/src/form-extensions/components/dataset-setting/`
按 section 拆分：

```
dataset-setting/
├── index.tsx                       // orchestrator（<100 行）
├── type.ts                         // DataSetInfo 重命名 + 新增字段
├── sections/
│   ├── BasicSection.tsx            // search strategy + topK（默认 10）
│   ├── QueryEnhancementSection.tsx // 4 个 checkbox + tooltip
│   ├── QueryInputSection.tsx       // query_image / query_mode 切换
│   ├── FilterSection.tsx           // target_chunk_types + filters
│   └── AdvancedSection.tsx         // retrievers + fusion_policy + retriever_params
└── components/
    ├── SliderArea / TitleArea / SearchStrategy   // 保留复用
    ├── DocumentIDsSelect                          // 删除整个文件
    └── （min_score slider JSX 在原 index 内删除）
```

### `DataSetInfo` 字段调整（`type.ts`）

- **删除**：`min_score`、`document_ids`、`use_nl2sql`、`is_personal_only`
- **重命名**：`use_rewrite` → `rewrite`、`use_rerank` → `enable_rerank`
- **新增**：`expansion`、`multi_query`、`query_image`、`query_mode`、
  `target_chunk_types`、`filters`、`retrievers`、`fusion_policy`、
  `retriever_params`

### 渲染顺序

```
Query 输入（DatasetParamsField，现有）
知识库选择（DatasetSelectField，现有）
<BasicSection>          ← 默认展开
  ├─ Search Strategy
  └─ Top K（slider，默认 10）
<QueryEnhancementSection>  ← 默认展开
  ├─ ☐ Query Rewrite     tooltip: 需 rag 部署配置 llm_base_url
  ├─ ☐ Query Expansion   tooltip 同上
  ├─ ☐ Multi Query       tooltip 同上
  └─ ☐ Enable Rerank     tooltip: 需 rag 部署配置 rerank_base_url
<QueryInputSection>      ← 默认折叠
  └─ Image Query toggle + 图片来源
<FilterSection>          ← 默认展开
  ├─ Target Chunk Types  [☐ text  ☐ image]
  └─ Filters             KV editor
<AdvancedSection>        ← 默认折叠（标题带 ⚠️ "需要 RAG 调参经验"）
  ├─ Retrievers          [☐ dense ☐ bm25 ☐ image_vector]
  ├─ Fusion Policy       JSON textarea + 校验
  └─ Retriever Params    JSON textarea（含 candidate_k 字段提示）
```

### `filters / fusion_policy / retriever_params` 的 UI 表达

- **`filters`**：KV editor。row = (key:string, value 类型选择
  string/number/array)；row 级红框校验类型不匹配；可增删行。
- **`fusion_policy`** / **`retriever_params`**：JSON textarea，blur 时
  `JSON.parse` 失败 → 红框 + 错误 tooltip，保存按钮 disable。

### 序列化（DataSetInfo → datasetParam[]）

前端表单存储沿用 `DatasetParam[]` 结构：每个字段一项 param，name 与
后端 Adapt 期望的 key 对齐：

```
{ name: "rewrite",         input: { type: bool,   value: {type:"literal", content: true} } }
{ name: "expansion",       input: { type: bool,   value: {type:"literal", content: false} } }
{ name: "multiQuery",      input: { type: bool,   value: {type:"literal", content: false} } }
{ name: "enableRerank",    input: { type: bool,   value: {type:"literal", content: true} } }
{ name: "filters",         input: { type: object, value: {type:"literal", content: {...}} } }
{ name: "targetChunkTypes",input: { type: list,   value: {type:"literal", content: ["text_chunk"]} } }
{ name: "retrievers",      input: { type: list,   value: {type:"literal", content: ["dense","bm25"]} } }
{ name: "fusionPolicy",    input: { type: object, value: {type:"literal", content: {...}} } }
{ name: "retrieverParams", input: { type: object, value: {type:"literal", content: {...}} } }
{ name: "queryMode",       input: { type: string, value: {type:"literal", content: "text_input"} } }
{ name: "queryImage",      input: { type: object, value: {type:"literal", content: {...}} } }
```

### 老 JSON 兼容

- 老节点 JSON 里残留的 `use_rewrite / use_rerank / min_score /
  document_ids / use_nl2sql / is_personal_only / max_tokens` 等
  param —— 前端 hydrate 时**静默忽略**（不显示、不弹错、不映射到
  新字段）
- 不做老字段 → 新字段的 transform；用户需要在新 UI 上重新勾选。
- 节点重新保存后老 param 不再被序列化，老字段从 JSON 中自然消失。

## 后端设计

### `backend/domain/workflow/internal/nodes/knowledge/knowledge_retrieve.go`

```go
type RetrieveConfig struct {
    KnowledgeIDs       []int64
    RetrievalStrategy  *knowledge.RetrievalStrategy
    ChatHistorySetting *vo.ChatHistorySetting
}
```

- **删除字段**：`DocumentIDs []int64`
- **`Adapt` 改动**：
  - 删除：`documentIDs / minScore / isPersonalOnly / useNl2sql` 4 个
    param 的解析分支
  - 重命名：`useRewrite` → `rewrite`、`useRerank` → `enableRerank`（param
    name 与前端对齐）
  - 新增：`expansion`、`multiQuery`、`queryImage`、`queryMode`、
    `targetChunkTypes`、`filters`、`retrievers`、`fusionPolicy`、
    `retrieverParams` 9 个 param 解析
  - 保留：`topK / strategy` 现有逻辑（topK 非正值 drop 守卫保留——rag
    仍要求 `top_k > 0`）
- **`Retrieve.Invoke`**：构造 `knowledge.RetrieveRequest` 时删除
  `DocumentIDs` 字段

### `backend/crossdomain/knowledge/model/retrieve.go` — `RetrievalStrategy`

字段重设：
- **删除**：`MinScore`、`MaxTokens`、`IsPersonalOnly`、`EnableNL2SQL`、
  `EnableQueryRewrite`、`EnableRerank`（后两个被重命名替换）
- **新增/重命名**：
  - `Rewrite bool`
  - `EnableRerank bool`
  - `Expansion bool`
  - `MultiQuery bool`
  - `QueryImage *QueryImage`
  - `QueryMode string`
  - `TargetChunkTypes []string`
  - `Filters map[string]any`
  - `Retrievers []string`
  - `FusionPolicy map[string]any`
  - `RetrieverParams map[string]any`
- **保留**：`TopK *int64`、`SearchType`

`RetrieveRequest`（顶层）：
- **删除**：`DocumentIDs`

### `backend/infra/contract/rag/types.go` — `RetrieveRequest`

- **删除字段**：`DocumentIDs`、`MinScore`、`MaxTokens`
- **保留**：`CandidateK *int`（公共 contract 类型可能有其他调用方；只
  在 knowledge-retrieve 链路上停止填充，不改 contract 形状）
- **删除注释**：lines 269-272 关于 "rag's pydantic validator caps
  document_ids at 200" 的 stale 注释

`QueryImage / QueryMode / Filters / TargetChunkTypes / Retrievers /
FusionPolicy / RetrieverParams / QueryStrategy` 已经在 struct 中存在，
无需新增。

### `backend/domain/knowledge/service/ragimpl/retrieval.go`

简化 `Impl.Retrieve`：

- **删除**：DocumentIDs 翻译 + mapping + "all unmapped → empty hits"
  short-circuit（约 lines 77-99）
- **删除**：MinScore / MaxTokens 转发（约 lines 118-134）
- **替换**：query rewrite / rerank 的 env-gated 逻辑（约 lines 148-183）
  改为直白的 4-boolean 转发：

  ```go
  if req.Strategy != nil {
      qs := map[string]any{}
      if req.Strategy.Rewrite      { qs["rewrite"] = true }
      if req.Strategy.Expansion    { qs["expansion"] = true }
      if req.Strategy.MultiQuery   { qs["multi_query"] = true }
      if req.Strategy.EnableRerank { qs["enable_rerank"] = true }
      if len(qs) > 0 {
          ragReq.QueryStrategy = qs
      }
  }
  ```

- **新增转发**：`QueryImage / QueryMode / TargetChunkTypes / Filters /
  Retrievers / FusionPolicy / RetrieverParams` 直接搬运到 `ragReq`
- **删除 `Impl` 字段**：`defaultLLMModelID`、`defaultRerankModelID`
- **删除 env 读取**：`RAG_DEFAULT_LLM_MODEL_ID`、
  `RAG_DEFAULT_RERANK_MODEL_ID` 在 constructor / 注入点的所有引用
- **删除守卫**：`MaxTokens < 1 → ErrKnowledgeInvalidParam`（字段没了）

### NL2SQL 守卫

`Impl.Retrieve` 入口处的 `req.Strategy.EnableNL2SQL → ErrRagFeaturePending
501` 整段守卫**彻底删除**——前端不再发，service 层这段防御变成死分支。

### service 层老字段彻底清理

跨层 grep + 删除（必须为空才算清理完成）：

- `RAG_DEFAULT_LLM_MODEL_ID` / `RAG_DEFAULT_RERANK_MODEL_ID`
- `defaultLLMModelID` / `defaultRerankModelID` 字段、setter、构造注入
- `IsPersonalOnly` / `EnableNL2SQL` 在 `domain/knowledge/service` 整个
  目录下的所有引用
- `MinScore` / `MaxTokens` / `DocumentIDs` 在
  `crossdomain/knowledge/model` 和 `domain/knowledge/service` 下的传播
  代码
- workflow Adapt 之外的所有调用方（如果有 agent / bot 侧的 retrieve
  调用直接 set 这些字段——需要顺手清理，确保 contract 一致）

## 错误处理

### 前端
- `fusion_policy / retriever_params` JSON 解析失败 → field 红框 +
  tooltip，保存 disable
- `filters` KV editor row 类型不匹配 → 行级红框
- `query_image` 引用上游变量但类型不匹配（不是 image / string）→
  表单红框
- 其余字段未输入 → omit 不发空串 / null，让 rag 用自己的默认

### 后端 Adapt
- 未找到 param → 不设字段（保持 zero value）
- 类型转换失败 → 返错（不静默丢）
- `queryMode` 限定 enum `text_input / image_input / mixed_input`；非法
  值返错
- `filters / fusionPolicy / retrieverParams` 必须是 `map[string]any`；
  type-assert 失败返错

### ragimpl 转发层
- 不再做"看 env 决定是否包字段"那种条件逻辑——非零值字段全部转发
- 错误透传 rag HTTP 错误码到 `ErrKnowledgeInvalidParam`

## 测试

### 后端单测（`knowledge_retrieve_test.go`）

**保留**（topK regression 守卫）：
- `TestAdapt_BlankTopKDoesNotEmitZero`
- `TestAdapt_ExplicitZeroTopKAlsoDropped`
- `TestAdapt_PositiveTopKIsKept`

**删除**：
- `TestAdapt_ParsesDocumentIDs`
- `TestAdapt_NoDocumentIDsKeepsNil`

**新增**：
- `TestAdapt_ParsesNewQueryStrategy` — 4 个 boolean 任意子集都落到
  `RetrievalStrategy` 对应字段
- `TestAdapt_ParsesFilters` — KV map 转 `map[string]any`，混合类型 value
- `TestAdapt_ParsesRetrievers` — `["dense","bm25"]` 落 list
- `TestAdapt_ParsesTargetChunkTypes` — 同上
- `TestAdapt_ParsesQueryImage` — `image_ref` / `image_base64` 至少一个
  非空
- `TestAdapt_RejectsInvalidQueryMode` — 非 enum 值返错
- `TestAdapt_IgnoresLegacyParams` — 老 JSON 带
  `useRewrite / useRerank / documentIDs / minScore / useNl2sql /
  isPersonalOnly` 时 Adapt 静默跳过、不报错、新字段都是 zero value

### ragimpl 测试

- query_strategy 4-boolean 子集正确序列化为 `query_strategy: {...}`
  不含多余 key
- `filters / fusion_policy / retriever_params` 非空时直接转发；空时
  omit
- 不再读 `RAG_DEFAULT_*_MODEL_ID` env（mock env 为空也能正常发
  `rewrite=true`）
- marshal 后 grep `"document_ids"` / `"min_score"` / `"max_tokens"` 不
  应出现

### 前端单测（vitest）

- 各 Section 组件独立渲染（snapshot）
- `data-transformer.ts`：DataSetInfo ↔ datasetParam[] 双向 round-trip
- 老 JSON 带老字段时 hydrate → DataSetInfo 不崩，新字段都是 default

### 端到端冒烟（人工）

按 `[[rag-stack-dev-stack-refs]]` 启动 dev stack：
- 构建节点 → 跑一次 workflow → 抓 coze→rag 的 `POST /api/v1/retrieval`
  body
- assert body 中：
  - 不出现 `document_ids / min_score / max_tokens / candidate_k`（顶层）
  - 不出现 `query_strategy.llm_model_id / rerank_model_id`
  - `query_strategy` 只含 4-key 子集
  - 新字段 `filters / retrievers / target_chunk_types / fusion_policy /
    retriever_params` 按 UI 输入正确出现

## 回滚与发布

- 改动是原子的：前后端必须一起部署
- 没有 feature flag——回滚 = revert PR
- 老工作流 JSON 在新代码下能加载、能跑（新字段 default、老字段忽略），
  但**保存后老字段从 JSON 中消失**（新前端不会再序列化它们）。属于
  可接受的一次性数据演进，与"不做 transform"决定一致
- 部署时机：避免与正在运行中的长 workflow 冲突——尽量在低峰期；运行
  中的 workflow 在新代码下执行 retrieve 调用时，老 strategy 字段被
  忽略，行为变化（之前会 send 一堆 silent-drop 字段，现在不 send），
  retrieval 结果可能与历史不同但语义上更正确

## 验证清单

发版前依次验证：

- [ ] `go test ./backend/domain/workflow/internal/nodes/knowledge/...` 通过
- [ ] `go test ./backend/domain/knowledge/...` 通过
- [ ] `rush test --to @coze-workflow/playground` 通过
- [ ] `grep -r "RAG_DEFAULT_LLM_MODEL_ID\|RAG_DEFAULT_RERANK_MODEL_ID" backend/` 为空
- [ ] `grep -r "EnableNL2SQL\|IsPersonalOnly" backend/domain/knowledge backend/crossdomain/knowledge` 为空
- [ ] `grep -rn "MinScore\|MaxTokens\|DocumentIDs" backend/domain/knowledge/service/ragimpl/retrieval.go` 为空
- [ ] 端到端冒烟：跑 knowledge-retrieve workflow，验证 wire body 形状
- [ ] 多个老工作流加载不崩、能保存
