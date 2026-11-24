-- 0052_asset_ignore.sql — 未登记终端忽略名单（M6-047）
--
-- 背景：打印机、AP、临时设备等不需要登记资产，但只要有租约就会一直出现在「未登记终端」，
-- 列表只增不减，真正的待登记终端被淹没。
--
-- 语义：按身份键（MAC 优先 / DUID 回退）抑制发现结果，可随时恢复；
-- 与 asset 分表，避免污染资产登记（忽略项没有使用人/部门/组织）。

CREATE TABLE IF NOT EXISTS asset_ignore (
  identity   text PRIMARY KEY,
  mac        text,
  duid       text,
  reason     text,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uniq_asset_ignore_mac ON asset_ignore (lower(mac)) WHERE mac IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uniq_asset_ignore_duid ON asset_ignore (duid) WHERE duid IS NOT NULL;
