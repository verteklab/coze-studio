# Document preview

Renderers for the knowledge "original file" preview panel.

## Currently supported

| Extension | Component / Hook | Notes |
| --------- | ----------------- | ----- |
| `md`      | `PreviewMd`       | Fetches text and renders in `<pre>`. |
| `txt`     | `PreviewTxt`      | Fetches text and renders in `<pre>`. |
| `docx`    | `PreviewDocx`     | Converts with `mammoth` in the browser, sanitized with DOMPurify. |
| `pdf`     | `usePreviewPdf`   | `react-pdf` / PDF.js with virtualized pages. Surfaces load errors via `onLoadError`. |

`doc` (legacy Word) is **not** rendered client-side. If present, `SegmentPreview`
falls back to `preview_tos_url` (a backend-converted PDF); otherwise the
workspace dispatcher shows an "unsupported" placeholder.

## Adding a new preview format

### Frontend (renderer)

1. Create `preview-<ext>.tsx` next to the existing ones. Accept `{ fileUrl }`,
   fetch, render, and show errors in-panel (see `preview-docx.tsx`).
2. Re-export from [`../index.tsx`](../index.tsx).
3. Add a `case` in the workspace dispatcher
   [`file-preview.tsx`](../../../../knowledge-ide-base/src/features/text-knowledge-workspace/components/file-preview.tsx)
   and the review dispatcher
   [`segment-preview/index.tsx`](../../../../knowledge-resource-processor-base/src/features/segment-preview/index.tsx).
4. If users should be able to upload the format, add the extension to
   `acceptFileTypes` / `fileFormatString` in
   [`constants/common.ts`](../../../../knowledge-resource-processor-base/src/constants/common.ts).

### Backend (ingest / parsing)

Only needed if the format also needs to be chunked into the knowledge base.

1. Add a `FileExtension*` constant and include it in `fileExtensionSet` in
   [`backend/infra/document/parser/manager.go`](../../../../../../../../backend/infra/document/parser/manager.go).
2. Add a `case` branch in
   [`backend/infra/document/parser/impl/builtin/manager.go`](../../../../../../../../backend/infra/document/parser/impl/builtin/manager.go)
   (and `impl/ppstructure/manager.go` if applicable) that returns a parser for
   the new extension. Put any Python helper under `impl/builtin/python/`.

### Server-side preview conversion (for formats with no browser renderer)

For formats like `.doc` or `.pptx` that can't be rendered in the browser:

1. After ingest, convert the upload to PDF (LibreOffice headless / unoconv) and
   upload the PDF artifact to object storage.
2. Populate `PreviewTosURL` with that artifact URL in
   [`convertDocument2Model`](../../../../../../../../backend/application/knowledge/convertor.go)
   and `MGetDocumentReview`
   ([`backend/domain/knowledge/service/knowledge.go`](../../../../../../../../backend/domain/knowledge/service/knowledge.go)).
3. Frontend dispatchers should route the extension to `usePreviewPdf` with the
   converted URL (see how `doc` is handled in `segment-preview/index.tsx`).
