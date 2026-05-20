-- Add image_url column to rag_doc_mapping.
-- Note: per coze-stack-atlas-declarative-deploy, this file is informational
-- only — mysql entrypoint runs atlas schema apply against the HCL, not this
-- migration. Kept here so an operator can reproduce the change manually if
-- needed.

ALTER TABLE `rag_doc_mapping`
  ADD COLUMN `image_url` VARCHAR(512) NULL DEFAULT NULL
    COMMENT 'Coze-side MinIO URL for image-source documents (for detail-page thumbnails). NULL for non-image docs and for pre-2026-05-20 image uploads.'
    AFTER `size`;
