-- 0056_asset_trgm_alignment.sql — 资产检索索引与查询表达式对齐（M6-064）
--
-- 0053 建的是 lower(col) 表达式索引，但查询使用 col ILIKE '%kw%'（且带 coalesce），
-- 表达式不一致导致 GIN 索引从未被使用，资产上万后退化为全表扫描。
-- 这里改为在列本身上建 pg_trgm GIN（pg_trgm 的 gin_trgm_ops 支持 ILIKE）。

DROP INDEX IF EXISTS idx_asset_owner_trgm;
DROP INDEX IF EXISTS idx_asset_dept_trgm;
DROP INDEX IF EXISTS idx_asset_mac_trgm;
DROP INDEX IF EXISTS idx_asset_duid_trgm;
DROP INDEX IF EXISTS idx_asset_mac_compact_trgm;

CREATE INDEX idx_asset_owner_trgm ON asset USING gin (owner gin_trgm_ops);
CREATE INDEX idx_asset_dept_trgm ON asset USING gin (dept gin_trgm_ops);
CREATE INDEX idx_asset_mac_trgm ON asset USING gin (mac gin_trgm_ops);
CREATE INDEX idx_asset_duid_trgm ON asset USING gin (duid gin_trgm_ops);
CREATE INDEX idx_asset_mac_compact_trgm ON asset USING gin (replace(lower(mac), ':', '') gin_trgm_ops);
