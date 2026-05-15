# coze 设计上支持但 rag 还不支持的功能清单

**最近更新：** 2026-05-15
**当前分支：** `feat/replace-knowledge-base` @ `4949711e`
**配套文档：** [`rag-replacement-progress-zh.md`](./rag-replacement-progress-zh.md) · [`coze-deployment-guide-zh.md`](./coze-deployment-guide-zh.md)

> 说明：本清单按"能力是否可用 / 如何降级"组织，便于 PM 评估和产品决策。所有引用的源码位置都基于本分支当前 HEAD。
>
> **2026-05-15 重要变更**：rag 端已完成 7 项能力（chunk CRUD、document metadata 更新、retrieval `document_ids` / `min_score` / `max_tokens`、task filename、rerank / query-rewrite 模型能力）。原 §A、§B、§C 大部分条目从"rag 端阻塞"变为"coze 端 wiring"。下面按新状态重排。

---

## 0. 本次更新影响的条目（速览）

| 原分类·项 | 旧状态 | 新状态 | 后续 slice |
|---|---|---|---|
| §A 切片手动管理 | rag 阻塞 | rag 已实现，待 coze 接通 | **R2-G**（spec 已就绪） |
| §A 文档元数据更新（`UpdateDocument`） | rag 阻塞 | rag 已实现 `POST /documents/{doc_id}/update` | **R2-H**（待写 spec） |
| §B `DocumentIDs` | rag 不支持 | rag `/retrieval` 顶层 `document_ids` 已收 | **R2-I**（小，单 commit） |
| §B `MinScore` | rag 不支持，coze 后置过滤 | rag `/retrieval` 顶层 `min_score` 已收 | **R2-J**（小） |
| §B `MaxTokens` | 静默 drop | rag `/retrieval` 顶层 `max_tokens` 已收（**近似**裁剪，见 §B 备注） | **R2-K**（小） |
| §B `EnableRerank` | 静默 drop | rag 端 rerank 能力已就绪 | **R2-F-Rerank**（已排队） |
| §C 上传进度页文档名 | rag task 不挂 filename | rag `GET /tasks/{task_id}` 已含 `filename` | **R2-L**（极小） |

未变化项继续保留在下面对应小节。

---

## A. 仍处于"调用即报 `105100001 feature pending rag support`" 的功能

> ragimpl 仍直接 stub 出去，对应入口的 UI 操作会失败。
> 来源：`backend/domain/knowledge/service/ragimpl/unsupported.go`

| 类别 | service 方法 | 用户场景 | 阻塞方 |
|---|---|---|---|
| **重新切片** | `ResegmentDocument` | 调整切分参数后对已有文档重切 | rag 端：需要规划"保留 doc_id，重跑 ingestion 流水线 + 替换全部 chunk"的语义 |
| **表格 / Sheet 类型摄入** | `GetAlterTableSchema` / `ValidateTableSchema` / `GetDocumentTableInfo` / `GetImportDataTableSchema` | xlsx / csv 作为结构化表导入 KB（列映射、表头校验） | rag 端：尚无 table chunk 数据模型 |
| **图片 caption 抽取** | `ExtractPhotoCaption` | 图片型 KB 的自动描述生成 | rag 端：未提供 caption 抽取流水线 |
| **文档审核（review）工作流** | `CreateDocumentReview` / `MGetDocumentReview` / `SaveDocumentReview` | 上传后人工审核切片再发布的流程 | rag 端：无该流程；coze 端也需重新评估产品价值 |
| **KB 复制 / 移动到 library** | `CopyKnowledge` / `MoveKnowledgeToLibrary` | KB 跨 space / library 复制 | rag 端：无原生 copy；coze 端可以"建新 KB + 重传文档"模拟，但代价高 |
| **NL2SQL 检索** | `Retrieve(Strategy.EnableNL2SQL=true)` | 工作流"知识库检索"节点的 SQL 生成模式 | rag 端：无 NL2SQL 模块；强绑定表格摄入 |

---

## A'. rag 端已实现、待 coze 接通（**主要工作面**）

> rag 端已完成相应 endpoint / 字段，coze 侧 ragimpl 还在 stub 或在做降级。每一条都对应一个独立的小 slice。

| 类别 | service 方法 / 字段 | rag endpoint 或字段 | coze 端要做的事 | slice |
|---|---|---|---|---|
| **切片手动管理** | `CreateSlice` / `UpdateSlice` / `DeleteSlice` / `ListSlice` / `GetSlice` / `MGetSlice` / `ListPhotoSlice` | `POST/GET /knowledgebases/{kb_id}/.../chunks*` 7 个（见 R2-G brief） | 新建 `rag_chunk_mapping` 表 + `ragimpl/slice.go` + Client DTO；顺带修 `Slice.Info.ID=0` 的窟窿 | **R2-G**（spec 已就绪，待 plan） |
| **文档元数据更新** | `UpdateDocument(DocumentName, TableInfo)` | `POST /knowledgebases/{kb_id}/documents/{doc_id}/update`（接受 `filename / tags / category / source_type / source_id / extra_metadata`） | 去 stub，新增 `ragimpl/document.go::UpdateDocument`；`TableInfo` 在表格摄入未做之前继续报错 | **R2-H** |
| **检索 `DocumentIDs` 过滤** | `Retrieve(Strategy.DocumentIDs)` | `/api/v1/retrieval` 顶层 `document_ids: [string]`（最多 200） | `ragimpl.Retrieve` 把 coze int64 doc id 通过 `rag_doc_mapping` 翻成 rag string doc id 数组写入 `ragReq.DocumentIDs`；删除现有 line 74-76 的 WARN | **R2-I** |
| **检索 `MinScore`** | `Retrieve(Strategy.MinScore)` | `/api/v1/retrieval` 顶层 `min_score: float` | `ragimpl.Retrieve` 写入 `ragReq.MinScore`；移除 service 层 post-filter（移除前确认现有调用方不依赖旧语义） | **R2-J** |
| **检索 `MaxTokens`** | `Retrieve(Strategy.MaxTokens)` | `/api/v1/retrieval` 顶层 `max_tokens: int` | `ragimpl.Retrieve` 写入 `ragReq.MaxTokens`；删除"静默 drop"路径。**注意**：rag 端是"近似"裁剪（按 chunk 边界，不是精确 token 计数），coze 端调用方若要严格预算，仍需在 service 层做精确二次裁剪 | **R2-K** |
| **检索 rerank** | `Retrieve(Strategy.EnableRerank)` | rag 端 rerank capability 已就绪，通过 `query_strategy.{enable_rerank, rerank_model_id}`（或 `fusion_policy`，待确认） | 类比 R2-F：加 `RAG_DEFAULT_RERANK_MODEL_ID` env + 翻译 `EnableRerank`；先与 rag 团队对齐字段名 | **R2-F-Rerank**（已排队） |
| **上传进度文档名** | `MGetDocumentProgress` | `GET /api/v1/tasks/{task_id}` 已含 `filename: Optional[str]` | `ragimpl/document.go::MGetDocumentProgress` 直接读 `task.filename`，删除 fallback 渲染 doc ID 的逻辑 | **R2-L** |

---

## B. 接受调用但参数被静默忽略 / 降级（残留项）

> 调用成功，但部分语义没在 rag 端生效。原 B 类大部分已经搬到 A' 表；剩余的：

| 检索 Strategy 字段 | 当前行为 | 损失 |
|---|---|---|
| `EnableQueryRewrite` 但 `RAG_DEFAULT_LLM_MODEL_ID` 为空 | drop rewrite + WARN（R2-F 已交付的行为） | 基础检索仍可用；运营只要配上 env var 即可恢复 |
| `ListPhotoSlice.HasCaption` | rag 的 `GET /knowledgebases/{kb_id}/chunks` 不接受该过滤；coze 端只能"取回全部 image chunk 后 post-filter"或 drop+WARN | image 数量大时浪费带宽 |

`ListPhotoSlice.HasCaption` 在 rag 端补 query 参数前是 R2-G 实现里需要处理的次要决策点；详见 R2-G brief §7 备注。

---

## C. UI 可见但 rag 形状错位 / 数据不全（残留项）

| 场景 | 缺什么 | 用户感知 |
|---|---|---|
| 工作流向导参数表单 | `GetCapabilities` + `ListDocumentParameterSchemas` 接好了，但 UI 还硬编码（**R2-D-fe-Wizard** queued） | embedding 模型可选项 / 分段策略选项 / chunk 参数是写死的，没跟 rag 能力同步 |
| Bucket-B UI 入口屏蔽 | A 类功能在 UI 上还能点（不是 404），点了才报 `105100001` | 用户体验差，需按 `kb.backend === "rag"` 屏蔽 A 类入口（其余 6 项进入 A' 后自然可用） |

> 原 C 节的"上传进度页文档名"、"切片 chunk-level ID 稳定性"已并入 A'（前者 → R2-L；后者由 R2-G 顺带修掉 `Slice.Info.ID=0`）。

---

## D. 设计上有，本次工作已实现（参考对照）

`service.Knowledge` 接口里 ragimpl 已经正常实现的方法：

- `CreateKnowledge` / `UpdateKnowledge` / `DeleteKnowledge` / `ListKnowledge` / `GetKnowledgeByID` / `MGetKnowledgeByID`
- `CreateDocument` / `DeleteDocument` / `ListDocument` / `MGetDocument` / `MGetDocumentProgress` / `RetryDocument`
- `Retrieve`（基本检索 + query rewrite via `RAG_DEFAULT_LLM_MODEL_ID`）
- `GetCapabilities` / `ListDocumentParameterSchemas`（后端已接，前端未消费）

---

## E. 消化优先级建议（2026-05-15 版）

按"修复 ROI 高 → 低 / 风险 → 阻塞依赖"重新排序：

| 优先级 | 切片 | 修复方式 | 工作量 | 备注 |
|---|---|---|---|---|
| 1 | **R2-I `DocumentIDs`** | 一行 wiring + 翻译 + 删 WARN | 极小（单 commit） | 用户最容易踩的语义错位 |
| 2 | **R2-J `MinScore`** | wiring + 移除 service 层 post-filter | 小 | 顺手把网络冗余裁掉 |
| 3 | **R2-K `MaxTokens`** | wiring + 文档化"近似"语义 | 小 | 调用方若需精确预算自行兜底 |
| 4 | **R2-L 进度页 filename** | 一行读 task.filename | 极小 | UX 立刻改善 |
| 5 | **R2-H `UpdateDocument`** | 删 stub + 走 `POST /documents/{doc_id}/update` | 小 | 解锁"改文档名 / 标签"基础能力 |
| 6 | **R2-F-Rerank** | 类比 R2-F：env + 字段翻译；先与 rag 对齐 `query_strategy` 字段名 | 小 | 命中质量直接收益 |
| 7 | **R2-G 切片手动管理** | 新建 `rag_chunk_mapping` 表 + 7 方法 + Client + 测试；顺带修 `Slice.Info.ID=0` | **中** | 单独最大的一项；spec 已就绪 |
| 8 | **Bucket-B UI 屏蔽** | 前端按 `kb.backend === "rag"` 屏蔽剩余 A 类入口（resegment / 表格 / caption / review / copy / nl2sql） | 小 | 在 1-7 落地之前先做也 OK，可与后端解耦 |
| 9 | **R2-D-fe-Wizard** | 工作流向导参数表单从硬编码改成 capabilities/schemas 驱动 | 大（UI 重构） | 不阻塞后端 |
| 10 | **A 类剩余项** | 看产品优先级；rag 端要先建对应能力（resegment / table / caption / review / copy / nl2sql） | 大；rag 端先行 | 产品决策 |

建议 1-6 可以**并发**推（不同文件，无相互依赖），R2-G 单独排一个迭代，前端 8、9 可与后端 1-7 并行。

---

## F. 相关源码 / spec 索引

- `backend/domain/knowledge/service/interface.go` — 完整 `service.Knowledge` 接口（包含 stub 和实现）
- `backend/domain/knowledge/service/ragimpl/unsupported.go` — A 类 stub 方法（21 个 → R2-G 落地后剩 14 个 → R2-H 落地后剩 13 个）
- `backend/domain/knowledge/service/ragimpl/retrieval.go:68-119` — B 类 strategy 处理（含 `DocumentIDs` WARN、`MinScore` post-filter 注释、`MaxTokens` drop、query rewrite 已 wired、`EnableRerank` 待 wired）
- `backend/domain/knowledge/service/ragimpl/document.go:306-346` — `MGetDocumentProgress` 当前实现，R2-L 改动落点
- `docs/superpowers/specs/2026-05-14-r2a-*` — bucket 划分原始 spec
- `docs/superpowers/specs/2026-05-14-r2f-retrieval-llm-model-id-design.md` — query rewrite 修复
- `docs/superpowers/specs/2026-05-15-r2g-manual-slice-design.md` — 切片手动管理 spec
- `docs/superpowers/specs/2026-05-15-r2g-manual-slice-api-brief.md` — R2-G 接口简表（rag 实际实现形状）
- `rag/app/api/routes/chunks.py` · `rag/app/api/routes/documents.py` · `rag/app/api/routes/retrieval.py` · `rag/app/api/routes/tasks.py` — rag 端实现位置
