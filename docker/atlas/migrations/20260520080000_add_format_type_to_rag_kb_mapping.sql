-- Add "format_type" column to "rag_kb_mapping"
-- Stores coze DocumentType (0=text, 1=table, 2=image) so hydrateKnowledge
-- can label a KB without round-tripping to rag — rag's KnowledgeBaseDetail
-- reports the union of every modality its embedding bindings support and
-- can't actually distinguish text/table/image KBs.
ALTER TABLE `rag_kb_mapping` ADD COLUMN `format_type` tinyint unsigned NOT NULL DEFAULT 0 COMMENT "coze DocumentType: 0=text, 1=table, 2=image";
