-- 0007_blocklist_idx.sql — 封禁名单索引（50 万条预算支撑，§8）
CREATE INDEX IF NOT EXISTS idx_entry_pattern ON blocklist_entry(pattern text_pattern_ops);
CREATE INDEX IF NOT EXISTS idx_entry_list ON blocklist_entry(list_id);
