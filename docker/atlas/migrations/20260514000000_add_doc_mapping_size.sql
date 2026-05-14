-- Modify "rag_doc_mapping" table
ALTER TABLE `opencoze`.`rag_doc_mapping` ADD COLUMN `size` bigint unsigned NOT NULL DEFAULT 0 COMMENT "Document file size in bytes; coze-side, since rag does not return size on its Document response.";
