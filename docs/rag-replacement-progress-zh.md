# coze-studio 知识模块替换为独立 rag 服务 —— 进度文档

**最近更新：** 2026-05-14
**当前分支：** `feat/replace-knowledge-base`（基于 main，未推送，未开 PR）
**当前 HEAD：** `0b5250b1`

---

## 1. 项目背景

把 coze-studio 内置的 `domain/knowledge/internal/*` 知识模块替换为独立的 `rag` Python/FastAPI 服务，通过 HTTP 调用。

**目标分工：**
- **rag**：拥有所有知识业务逻辑（数据持久化、租户隔离、文档解析/切片/嵌入/索引、检索）
- **coze**：变成展示层 + 工作流编排层，通过 `KNOWLEDGE_BACKEND=rag` 特性开关切换后端

**当前状态：** Phase 1（基础结构）+ Phase 1.5（UI 流程对齐）+ R2-A..R2-F（合约重对齐 + 端到端打通）全部已落地。**rag-backed 知识库的完整生命周期已端到端打通：创建 → 上传 → 索引 → 列表/详情 → 失败重试 → 工作流检索（含 query rewrite）。**

---

## 2. 已完成切片

### Phase 1 + Phase 1.5（早于本次工作）
- 全部 Go 代码骨架（`infra/rag` 客户端、`infra/contract/rag` 类型契约、`ragimpl` 域实现、`conf/rag` 配置）
- `rag_kb_mapping` + `rag_doc_mapping` 两张 mapping 表（Atlas HCL 声明）
- 特性开关 `KNOWLEDGE_BACKEND=legacy|rag`（默认 legacy，legacy 路径验证仍可用）
- 创建 KB 弹窗里的 `<ModelSelector />` 组件 + IDL 扩展（embedding model id 字段）
- C-path（第一轮合约审计）：15 处接口契约不匹配，全部修复
- Phase 1.5：rag-backed KB 走 2 步 `[upload, progress]` 简化上传向导，避开 legacy 的 `CreateDocumentReview` 等死路径

### Round-2 审计修复（本次工作的主体）

针对 rag `0e1f49b` 的接口形变做的契约对齐 + 端到端打通：

| 切片 | commits | 干了什么 | 验证 |
|---|---|---|---|
| **R2-A** | `c4ebc13a` + `4f6f6a48` + `e5666832` + `78702f6a` | `CreateDocument` 从 JSON-with-source_uri 改成 multipart-with-bytes；ragimpl 加 `storage.Storage` 注入，从 MinIO 取 bytes 后转发给 rag；httptest 锁线上形状 | ✅ 单测 + 端到端 smoke（上传 → rag task=success doc=ready） |
| **R2-B** | 8 个 commit（含 atlas migration） | 读路径 DTO 对齐：`Task` + `Document` 字段重命名 / 删旧字段 / 新字段；`progressForStatus` 粗粒度从 task.Status 推导进度；`Size` 列加到 `rag_doc_mapping`、上传时持久化、读路径回填 | ✅ 单测 + smoke（UI 显示真实文件名/类型/大小，进度条 10→50→100 跳变）|
| **R2-C** | `8c236a24` + `be3d3d22` + `b0669212` + `3729a8bb` | Union-friendly 错误信封 decoder（三种 shape：flat / FastAPI HTTPException / pydantic 422 array）；`Retrieve.query_image` 从 `*string` 改对象类型 `*QueryImage{ImageBase64, ImageRef}` | ✅ 单测 + httptest + 手动 curl 探针 + **生产环境野生验证**：工作流日志显示 `rag 40004: query_strategy.llm_model_id is required` 这类错误现在能被 decoder 漂亮 surface 出来 |
| **R2-D-backend** | 5 个 commit | 三个新 rag 端点接入 ragimpl 层（不上 `service.Knowledge` 接口）：`RetryDocument`、`GetCapabilities` + `KBCapabilities` DTO、`ListDocumentParameterSchemas` + nested DTO + 新文件 `parameter_schemas.go` | ✅ 单测 + httptest |
| **R2-D-fe-Retry** | `7834e622` + `75035b8c` | 失败上传重试按钮端到端打通：service 接口 + ragimpl 签名重对齐 + `mapping.UpdateLastTaskID` 新 helper + legacy stub + application handler（带 uid+permission 门控）+ 新 IDL RPC `/api/knowledge/document/retry` + 手工编辑 Go binding (~513 行) + 手工编辑 TS binding + Hertz 路由 + 前端按钮 enable + 3 个前端测试。**17 个代码文件 + 2 个文档。** | ✅ 单测 + 前端测试；live UI smoke 被 rag config cache 多层失效阻挠（见已知限制）|
| **R2-F** | `d1f110e2` + `0b5250b1` | 工作流检索节点 query rewrite 修复：加 `RAG_DEFAULT_LLM_MODEL_ID` 环境变量 → 配置 → ragimpl 字段；`retrieval.go` rewrite 分支带 `llm_model_id`；env 为空时 drop + WARN 日志保证基本检索仍可用 | ✅ 单测 + **live 验证**：`POST /api/v1/retrieval 200` 耗时 3.7s（vs 之前 ms 级 40004 reject）|

---

## 3. 端到端能力矩阵

| 能力 | 状态 | 说明 |
|---|---|---|
| 创建 rag KB（含 embedding model 选择）| ✅ | Phase 1 + Phase 1.5 |
| 上传文档（含 multipart + MinIO 取 bytes + 索引 + 状态轮询）| ✅ | R2-A + R2-B |
| KB 详情 / 文档列表（filename / size / type / chunk_count）| ✅ | R2-B 字段重对齐 |
| 上传进度条（10% → 50% → 100% 跳变）| ✅ | R2-B `progressForStatus` 粗粒度映射 |
| 上传失败后"重试"按钮（前端 + 后端）| ✅ | R2-D-fe-Retry，单测 + 前端测试覆盖 |
| 错误诊断（rag 端的真实 code/message 透传给上游）| ✅ | R2-C decoder |
| 工作流"知识库检索"节点（基本检索）| ✅ | Phase 1 时已通 |
| **工作流"知识库检索"节点 + query rewrite** | ✅ | R2-F，live 验证 |
| 工作流"知识库检索"节点 + rerank | ❌ | R2-F-Rerank queued（rag 端需先注册 rerank 模型）|
| 文档级 doc_ids 过滤 | ❌ | R2-A queued #3，依赖 rag filters 支持 |
| `MinScore` / `MaxTokens` 翻译 | ⚠️ 仅 coze 端 post-filter | R2-A queued #3 |
| 知识库切片管理 UI（手动切片增删改查）| ❌ | bucket-B stub（rag 不暴露 chunk API），queued item #12 |
| 知识库复制/移动 / 文档元数据更新 / 重新切片 | ❌ | bucket-B stub，queued item #12 |

---

## 4. 队列中（未做）

按优先级排：

| 切片 | 说明 | 大小 |
|---|---|---|
| **R2-D-fe-Wizard** | 同 R2-D-fe-Retry 的 5 层穿透模式，把 `GetCapabilities` + `ListDocumentParameterSchemas` 暴露到工作流向导。前端从硬编码参数表单改成 capabilities/schemas 驱动 | 大（UI 重构） |
| **R2-F-Rerank** | 类比 R2-F 的最小修复，加 `RAG_DEFAULT_RERANK_MODEL_ID` env 并翻译 `EnableRerank` → `query_strategy.{enable_rerank, rerank_model_id}` | 小（需先在 rag 注册 rerank 模型）|
| **R2-E** | 给 R2-A..R2-F 之外的 rag 端点补 httptest；扩 `rag-contract-check` 校验 body 形状 | 小到中 |
| **Bucket-B UI 屏蔽**（queued #12）| `kb.backend === "rag"` 时屏蔽切片管理、KB 复制/移动、文档元数据等不支持的入口 | 小 |
| **PR-1** | 把整个 `feat/replace-knowledge-base` 分支开 PR 到 main | 流程 |
| **PR-2**（spec 推迟）| `KNOWLEDGE_BACKEND` 默认翻到 `rag`，删除 legacy 模块 | 大（清理）|

---

## 5. 已知运维注意事项 / 限制

1. **Toolchain 锁定**：`bytedance/sonic v1.14` 不兼容本地 Go 1.26。所有 `go` 命令必须 `GOTOOLCHAIN=go1.24.0` 前缀。
2. **rag MinIO bucket 不自动创建**：fresh MinIO volume 起来后第一次上传会 500（`NoSuchBucket: rag-files`）。需手动 `docker run --rm --network rag_default --entrypoint sh minio/mc:latest -c "mc alias set rag http://minio:9000 minioadmin minioadmin && mc mb rag/rag-files"`。
3. **rag config 多层缓存难失效**：编辑 `rag/config/model_providers.json` 后即便 `docker compose restart web worker`，新上传的 doc 仍然把旧 model_name 快照进 `documents.processing_config`。要可靠注入失败状态走的不是配置层，而是 `db.tasks.updateOne({_id:"..."}, {$set:{status:"failed"}})`。
4. **rag 内部 mongo 用 `_id` 做主键**，不是 `task_id` / `doc_id` 字段。Debug 查询：`db.tasks.findOne({_id: "<uuid>"})`，不是 `findOne({task_id: ...})`。
5. **Atlas migrations 替换 bug（先前遗留）**：`20260513010712_add_rag_mapping.sql` 表名没加 `opencoze.` 前缀，导致 `atlas migrate diff/validate` 跑不起来。生产部署用 `make sync_db`（declarative `atlas schema apply`）绕过；这条限制不影响生产，但影响后续 schema 变更工具链。R2-B 的 size 列迁移用了手写补充方案（`9edd5eb3`）。
6. **IDL codegen toolchain 未配置**：本仓库没有 `make idl` 类目标。IDL 改动需手工编辑生成的 Go binding（`backend/api/model/data/knowledge/*.go`，~每个 RPC 500 行）和 TS binding（`frontend/packages/arch/idl/src/auto-generated/knowledge/*`），并加 `// MANUAL EDIT:` 注释（参考 `e2dcc807`/`1462ebd9`）。
7. **`MGetDocumentProgress` 不返回 `document_name`**：ragimpl 这个方法只调 `GetTask`（rag 不在 task 上挂 filename），所以前端 `<UploadProgressPoll />` 拿不到文件名，fallback 渲染 doc ID。UX 可接受，未来若要补，需要从 mapping 表 fetch 或加 `GetDocument` 调用。
8. **重试时 `last_task_id` 必须更新**：R2-D-fe-Retry 在 `ragimpl.RetryDocument` 里同步更新 `rag_doc_mapping.last_task_id` 到新 task。否则进度轮询会一直读旧的失败 task。

---

## 6. 分支提交链（自 `67b73042` Phase 1.5 之后）

```
0b5250b1 docs(rag): add R2-F spec and implementation plan
d1f110e2 feat(rag): wire query_strategy.llm_model_id for retrieval enhancement
75035b8c chore(knowledge): R2-D-fe-Retry post-review cleanup
7834e622 feat(knowledge): enable document retry end-to-end
e2910b04 docs(rag): add R2-D-backend spec and implementation plan
0ccc3f2a feat(rag): wire ListDocumentParameterSchemas endpoint
7282938f feat(rag): wire GetCapabilities endpoint
a82bfd46 style(ragimpl): gofmt re-align fakeClient field block
bfdf4e6e feat(rag): wire RetryDocument endpoint
3729a8bb docs(rag): rewrite stale R2-C forward-reference comment
b0669212 docs(rag): add R2-C spec and implementation plan
be3d3d22 refactor(rag): switch Retrieve.query_image to object shape
8c236a24 refactor(rag): union-friendly error envelope decoder
9edd5eb3 chore(atlas): add numbered migration for rag_doc_mapping.size
8c58fc64 docs(rag): add R2-B spec and implementation plan
996e365f refactor(ragimpl): reorder InsertDoc args; extract Document builder
d5ddd983 feat(ragimpl): persist document file size on the coze side
0705e20b test(rag): lock decoded times + flag unchecked FileExtension cast
0746fea9 refactor(rag): realign Document contract; populate Filename + FileType
389c77e1 test(rag): assert decoded time values in TestGetTask_FieldShape
537ab835 refactor(rag): realign Task contract + coarse progress derivation
78702f6a docs(rag): add R2-A spec and implementation plan
e5666832 feat(ragimpl): fetch bytes from MinIO before rag CreateDocument
4f6f6a48 refactor(rag): drop unused method param from doMultipart
c4ebc13a refactor(rag): switch CreateDocument to rag's multipart contract
67b73042 feat(knowledge): gate upload wizard config by kb.backend  ← Phase 1.5 末端
```

26 个 commit 在分支上，分支没推送，没开 PR。

---

## 7. 相关 specs / plans 路径

每个切片都有详细的 spec + implementation plan：

- `docs/superpowers/specs/2026-05-12-replace-knowledge-module-with-rag-design.md`（Phase 1 总设计）
- `docs/superpowers/specs/2026-05-13-coze-ui-rag-flow-alignment-design.md`（Phase 1.5）
- `docs/superpowers/specs/2026-05-14-r2a-createdocument-multipart-design.md`
- `docs/superpowers/specs/2026-05-14-r2b-readpath-realignment-design.md`
- `docs/superpowers/specs/2026-05-14-r2c-retrieve-and-error-decoder-design.md`
- `docs/superpowers/specs/2026-05-14-r2d-backend-three-endpoints-design.md`
- `docs/superpowers/specs/2026-05-14-r2d-fe-retry-design.md`
- `docs/superpowers/specs/2026-05-14-r2f-retrieval-llm-model-id-design.md`

对应 plan 在 `docs/superpowers/plans/`。
