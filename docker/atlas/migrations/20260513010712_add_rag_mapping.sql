-- Create "rag_doc_mapping" table
CREATE TABLE `rag_doc_mapping` (
  `coze_doc_id` bigint unsigned NOT NULL COMMENT "coze int64 document id",
  `rag_doc_id` varchar(64) NOT NULL COMMENT "rag UUID (authoritative)",
  `coze_kb_id` bigint unsigned NOT NULL COMMENT "owning KB (FK to rag_kb_mapping.coze_kb_id)",
  `creator_id` bigint unsigned NOT NULL DEFAULT 0 COMMENT "informational: creator user id",
  `last_task_id` varchar(64) NULL COMMENT "rag task id from last ingestion attempt (used by MGetDocumentProgress)",
  `created_at` bigint unsigned NOT NULL DEFAULT 0 COMMENT "Create Time in Milliseconds",
  `deleted_at` datetime(3) NULL COMMENT "Delete Time",
  PRIMARY KEY (`coze_doc_id`),
  INDEX `idx_kb` (`coze_kb_id`, `deleted_at`),
  UNIQUE INDEX `uk_rag_doc_id` (`rag_doc_id`)
) CHARSET utf8mb4 COLLATE utf8mb4_unicode_ci COMMENT "Map coze int64 document id to rag UUID + rag task tracking. Name/status/source_uri live in rag.";
-- Create "rag_kb_mapping" table
CREATE TABLE `rag_kb_mapping` (
  `coze_kb_id` bigint unsigned NOT NULL COMMENT "coze int64 knowledge id",
  `rag_kb_id` varchar(64) NOT NULL COMMENT "rag UUID (authoritative)",
  `icon_uri` varchar(255) NULL COMMENT "coze-only display field; rag has no icon concept",
  `app_id` bigint unsigned NOT NULL DEFAULT 0 COMMENT "informational: project/app id (used for UI filter only; never gates isolation)",
  `creator_id` bigint unsigned NOT NULL DEFAULT 0 COMMENT "informational: creator user id",
  `created_at` bigint unsigned NOT NULL DEFAULT 0 COMMENT "Create Time in Milliseconds",
  `deleted_at` datetime(3) NULL COMMENT "Delete Time",
  PRIMARY KEY (`coze_kb_id`),
  INDEX `idx_app` (`app_id`, `deleted_at`),
  UNIQUE INDEX `uk_rag_kb_id` (`rag_kb_id`)
) CHARSET utf8mb4 COLLATE utf8mb4_unicode_ci COMMENT "Map coze int64 KB id to rag UUID + coze-only display fields (icon, audit). Name/description/status live in rag.";
