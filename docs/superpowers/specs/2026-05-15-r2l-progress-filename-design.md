# R2-L: 上传进度页文档名从 `task.filename` 取，去掉 doc ID fallback

**Date:** 2026-05-15
**Status:** Draft
**Predecessor:** R2-F (2026-05-14)
**Sibling slices:** R2-H / R2-I / R2-J / R2-K / R2-F-Rerank / R2-G —— 同期可并行
**Companion gap:** `docs/rag-feature-gaps-zh.md` §A' 行 "上传进度文档名"

## 1. Motivation

`backend/domain/knowledge/service/ragimpl/document.go:306-339` 实现的 `MGetDocumentProgress` 只调 `rag.GetTask`，不调 `GetDocument`。`service.DocumentProgress.Name` 字段从未被填，前端 `<UploadProgressPoll />` 兜底渲染 doc ID 而非文件名。原 gap 文档 §C 第一行就是这个问题。

Rag 端 `TaskDetail` schema 已暴露 `filename: Optional[str]`（`rag/app/api/schemas/task.py:16`，`rag/app/api/routes/tasks.py:33` 写入）。R2-L 只需要在 coze 侧的 `contract.Task` 加字段，并在 `MGetDocumentProgress` 把它读出来填到 `DocumentProgress.Name`。

## 2. Goals & non-goals

### Goals

- `backend/infra/contract/rag/types.go::Task` 加 `Filename *string \`json:"filename,omitempty"\``（指针因为 rag 端是 Optional，未知阶段可能为 null）。
- `backend/domain/knowledge/service/ragimpl/document.go::MGetDocumentProgress` 在拼装 `DocumentProgress` 时：
  ```go
  if task.Filename != nil {
      dp.Name = *task.Filename
  }
  ```
- 未启动 task（`m.LastTaskID == ""`）的分支保持 `dp.Name == ""`（无 task 则无 filename；前端兜底逻辑仍可用）。
- Unit test：filename present / filename nil / 多 doc 混合 / mapping 缺失 doc 不影响其他。
- httptest：lock GetTask response 含 `filename` 字段。

### Non-goals

- 改 `service.DocumentProgress` 形状（`Name string` 已存在，本 slice 只填它）。
- 在 mapping 表上挂 filename（曾在 op note #18 讨论过，但既然 rag 直接给就不需要 coze 端缓存）。
- 前端改动。`<UploadProgressPoll />` 现有的 `name || id` 兜底逻辑保持不变；本 slice 让 `name` 字段不再是空字符串。

## 3. Contract change

### 3.1 Task DTO 加字段

`backend/infra/contract/rag/types.go::Task`：

```go
type Task struct {
    TaskID     string   `json:"task_id"`
    Type       string   `json:"type"`
    Status     string   `json:"status"`
    RetryCount int      `json:"retry_count"`
    ErrorMsg   string   `json:"error_msg,omitempty"`
    Filename   *string  `json:"filename,omitempty"`   // NEW
    CreatedAt  RagTime  `json:"created_at"`
    StartedAt  *RagTime `json:"started_at,omitempty"`
    FinishedAt *RagTime `json:"finished_at,omitempty"`
}
```

### 3.2 MGetDocumentProgress 改一行

`document.go:333-336` 附近：

```go
dp.Status = taskStatusToDoc(task.Status)
dp.Progress = progressForStatus(task.Status)
dp.StatusMsg = task.ErrorMsg
if task.Filename != nil {
    dp.Name = *task.Filename            // NEW
}
list = append(list, dp)
```

## 4. Files touched

| File | Change |
|---|---|
| `backend/infra/contract/rag/types.go` | Task 加 `Filename *string` |
| `backend/infra/rag/client_test.go` | httptest 锁 GetTask response 含 filename |
| `backend/domain/knowledge/service/ragimpl/document.go` | 一行 `dp.Name = ...` |
| `backend/domain/knowledge/service/ragimpl/document_test.go` | unit test：filename present / nil / 混合 |

## 5. Testing

Unit：
- `TestMGetDocumentProgress_Filename_Set` — fake client `GetTask` 返回 `Filename: ptr("doc.pdf")`，结果 `DocumentProgress.Name == "doc.pdf"`。
- `TestMGetDocumentProgress_Filename_Nil` — fake client 返回 `Filename: nil`，结果 `Name == ""`。
- `TestMGetDocumentProgress_MixedFilenames` — 三个 doc，两个有 filename，一个无 → 各自 Name 正确。
- 现有 `TestMGetDocumentProgress_LastTaskIDEmpty` 不变（`Name == ""` 仍是正确行为）。

httptest：lock `GET /api/v1/tasks/{task_id}` response 含 `"filename": "doc.pdf"` 字段，coze 端 unmarshal 后 `task.Filename == ptr("doc.pdf")`。

## 6. Compatibility & rollout

- DTO additive。指针 + omitempty 保证旧 rag 部署（如果还有未升级版本回传 task 不含 filename）安全。
- 前端无改动 —— 它已经在用 `name`，只是过去拿到空字符串走 fallback；现在拿到真名字直接渲染。
- 解决项目 memory 的 op note #18 "MGetDocumentProgress response leaves document_name empty"。删 op note 时同步指向本 slice。
