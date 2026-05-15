# coze 设计上支持但 rag 还不支持的功能清单

**最近更新：** 2026-05-14
**当前分支：** `feat/replace-knowledge-base` @ `4949711e`
**配套文档：** [`rag-replacement-progress-zh.md`](./rag-replacement-progress-zh.md) · [`coze-deployment-guide-zh.md`](./coze-deployment-guide-zh.md)

> 说明：本清单按"能力是否可用 / 如何降级"组织，便于 PM 评估和产品决策。所有引用的源码位置都基于本分支当前 HEAD。

---

## A. 调用即报 `105100001 feature pending rag support`

> ragimpl 直接 stub 出去，对应入口的 UI 操作会失败。
> 来源：`backend/domain/knowledge/service/ragimpl/unsupported.go`

| 类别 | service 方法 | 用户场景 |
|---|---|---|
| **切片手动管理** | `CreateSlice` / `UpdateSlice` / `DeleteSlice` / `ListSlice` / `GetSlice` / `MGetSlice` / `ListPhotoSlice` | 知识库详情页 → 文档 → 查看 / 编辑 / 增删 chunk |
| **文档元数据更新** | `UpdateDocument` | KB 内修改文档名、标签、自定义字段 |
| **重新切片** | `ResegmentDocument` | 调整切分参数后对已有文档重切 |
| **表格 / Sheet 类型摄入** | `GetAlterTableSchema` / `ValidateTableSchema` / `GetDocumentTableInfo` / `GetImportDataTableSchema` | xlsx / csv 作为结构化表导入 KB（列映射、表头校验） |
| **图片 caption 抽取** | `ExtractPhotoCaption` | 图片型 KB 的自动描述生成 |
| **文档审核（review）工作流** | `CreateDocumentReview` / `MGetDocumentReview` / `SaveDocumentReview` | 上传后人工审核切片再发布的流程 |
| **KB 复制 / 移动到 library** | `CopyKnowledge` / `MoveKnowledgeToLibrary` | KB 跨 space / library 复制 |
| **NL2SQL 检索** | `Retrieve(Strategy.EnableNL2SQL=true)` | 工作流"知识库检索"节点的 SQL 生成模式 |

---

## B. 接受调用但参数被静默忽略 / 降级

> 调用成功，但部分语义没在 rag 端生效。
> 来源：`backend/domain/knowledge/service/ragimpl/retrieval.go`

| 检索 Strategy 字段 | 当前行为 | 损失 |
|---|---|---|
| `DocumentIDs` | 记 WARN，rag 端不过滤 | KB 内"只在选定文档中搜"失效，命中范围是整库 |
| `MinScore` | rag 不收，coze 端调用 service 时自己 post-filter | rag 仍返回全量 TopK，coze 端裁剪——浪费带宽，但语义正确 |
| `MaxTokens` | 静默 drop | 命中拼接超 token 预算的情况不会被预先裁掉 |
| `EnableRerank` | 静默 drop（R2-F-Rerank queued） | rerank 阶段不生效，仅有 fusion 后的原始排序 |
| `EnableQueryRewrite` 但 `RAG_DEFAULT_LLM_MODEL_ID` 为空 | drop rewrite + WARN | 基础检索仍可用，但 query 改写未发生 |

---

## C. UI 可见但 rag 形状错位 / 数据不全

| 场景 | 缺什么 | 用户感知 |
|---|---|---|
| 上传进度页文档名 | `MGetDocumentProgress` 只调 rag `GetTask`，rag 不在 task 上挂 filename | `<UploadProgressPoll />` fallback 渲染 doc ID 而非文件名 |
| 工作流向导参数表单 | `GetCapabilities` + `ListDocumentParameterSchemas` 接好了，但 UI 还硬编码（R2-D-fe-Wizard queued） | embedding 模型可选项 / 分段策略选项 / chunk 参数是写死的，没跟 rag 能力同步 |
| 切片 chunk-level ID 稳定性 | rag 用 string uuid，coze service 层 `Slice.Info.ID` 是 int64，目前留 0 | 命中结果里 chunk 没有持久 ID，无法回链跳转 |
| Bucket-B UI 入口屏蔽 | A 类功能在 UI 上还能点（不是 404），点了才报 `105100001` | 用户体验差，需按 `kb.backend === "rag"` 屏蔽（queued item #12） |

---

## D. 设计上有，本次工作已实现（参考对照）

`service.Knowledge` 接口里 ragimpl 已经正常实现的方法，列出来便于对照：

- `CreateKnowledge` / `UpdateKnowledge` / `DeleteKnowledge` / `ListKnowledge` / `GetKnowledgeByID` / `MGetKnowledgeByID`
- `CreateDocument` / `DeleteDocument` / `ListDocument` / `MGetDocument` / `MGetDocumentProgress` / `RetryDocument`
- `Retrieve`（基本检索 + query rewrite）
- `GetCapabilities` / `ListDocumentParameterSchemas`（后端已接，前端未消费）

---

## E. 消化优先级建议

| 优先级 | 切片 | 修复方式 | 工作量 |
|---|---|---|---|
| 1 | **R2-F-Rerank** | 类比 R2-F：加 `RAG_DEFAULT_RERANK_MODEL_ID` env + 翻译 `EnableRerank` → `query_strategy.{enable_rerank, rerank_model_id}` | 小（单 commit），需先在 rag 注册 rerank 模型 |
| 2 | **Bucket-B UI 屏蔽** | 前端按 `kb.backend === "rag"` 屏蔽 A 类入口 | 小 |
| 3 | **R2-D-fe-Wizard** | 工作流向导参数表单从硬编码改成 capabilities/schemas 驱动 | 大（UI 重构） |
| 4 | **DocumentIDs filter** | 等 rag `/retrieval` 加 `filters` 字段后接通 | 取决于 rag 进度 |
| 5 | **表格 / 图片 / review / copy 类** | 看产品优先级；rag 端要先建对应能力 | 大；rag 端先行 |

---

## F. 相关源码 / spec 索引

- `backend/domain/knowledge/service/interface.go` — 完整 `service.Knowledge` 接口（包含 stub 和实现）
- `backend/domain/knowledge/service/ragimpl/unsupported.go` — A 类 stub 方法（21 个）
- `backend/domain/knowledge/service/ragimpl/retrieval.go:74-119` — B 类 strategy 降级逻辑
- `docs/superpowers/specs/2026-05-14-r2a-*` — bucket 划分原始 spec
- `docs/superpowers/specs/2026-05-14-r2f-*` — R2-F 修复设计（rewrite 路径）
