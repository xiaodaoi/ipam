-- 0053_asset_trgm.sql — 资产关键字检索索引（M6-048）
--
-- 背景：资产列表改为服务端分页 + 服务端统计后，关键字检索必须走索引；
-- `%kw%` 形式 ILIKE 走不了 B-tree，需 pg_trgm GIN 索引（组织规模到千级后差异明显）。
--
-- 说明：MAC 支持省略分隔符输入，额外为 replace(lower(mac),':','') 建表达式索引。

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS idx_asset_owner_trgm ON asset USING gin (lower(owner) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_asset_dept_trgm ON asset USING gin (lower(dept) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_asset_mac_trgm ON asset USING gin (lower(mac) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_asset_duid_trgm ON asset USING gin (duid gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_asset_mac_compact_trgm ON asset USING gin (replace(lower(mac), ':', '') gin_trgm_ops);
