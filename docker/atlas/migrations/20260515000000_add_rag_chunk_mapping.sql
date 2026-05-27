-- Create "rag_chunk_mapping" table
CREATE TABLE `rag_chunk_mapping` (
  `coze_slice_id` bigint unsigned NOT NULL COMMENT "coze int64 slice (chunk) id",
  `rag_chunk_id` varchar(64) NOT NULL COMMENT "rag chunk UUID (authoritative)",
  `rag_doc_id` varchar(64) NOT NULL COMMENT "rag doc UUID owning this chunk",
  `coze_doc_id` bigint unsigned NOT NULL COMMENT "owning coze document (FK to rag_doc_mapping.coze_doc_id)",
  `created_at` bigint unsigned NOT NULL DEFAULT 0 COMMENT "Create Time in Milliseconds",
  `deleted_at` datetime(3) NULL COMMENT "Delete Time",
  PRIMARY KEY (`coze_slice_id`),
  INDEX `idx_rag_chunk_id` (`rag_chunk_id`),
  INDEX `idx_coze_doc_id` (`coze_doc_id`, `deleted_at`),
  INDEX `idx_rag_doc_id` (`rag_doc_id`, `deleted_at`)
) CHARSET utf8mb4 COLLATE utf8mb4_unicode_ci COMMENT "Map coze int64 slice id to rag chunk UUID. Concurrency-safe lazy backfill on read paths permits short-lived multiple coze_slice_ids per rag_chunk_id; resolution policy is read-earliest (no UNIQUE on rag_chunk_id).";
