-- 0051_asset_identity.sql — 资产身份键扩展（M6-046）
--
-- 背景：coherence_binding 里 IPv6-only 终端以 DUID 作为 mac 列、ipv4=0.0.0.0，
-- 而 asset 以 mac 为主键且要求 12 位十六进制，导致这类终端既发现不了也登记不了（实测 170/278 台）。
--
-- 变更：
--   1) 新增 identity（身份键，MAC 优先 / DUID 回退）作为主键；
--   2) 新增 duid 列（唯一），mac 允许为空（DUID-only 终端）；
--   3) 补 org_id 过滤索引（资产页按组织筛选）。

ALTER TABLE asset ADD COLUMN IF NOT EXISTS duid text;
ALTER TABLE asset ADD COLUMN IF NOT EXISTS identity text;

-- 存量行：mac 必然存在，统一小写后作为身份键（应用层写入即归一化格式）。
UPDATE asset SET identity = lower(mac) WHERE identity IS NULL AND mac IS NOT NULL;
UPDATE asset SET identity = duid WHERE identity IS NULL AND duid IS NOT NULL;

ALTER TABLE asset ALTER COLUMN identity SET NOT NULL;

ALTER TABLE asset DROP CONSTRAINT IF EXISTS asset_pkey;
ALTER TABLE asset ADD CONSTRAINT asset_pkey PRIMARY KEY (identity);

ALTER TABLE asset ALTER COLUMN mac DROP NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uniq_asset_mac ON asset (lower(mac)) WHERE mac IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uniq_asset_duid ON asset (duid) WHERE duid IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_asset_org ON asset (org_id);
