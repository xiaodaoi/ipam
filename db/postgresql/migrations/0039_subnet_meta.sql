-- 0039: 子网级元数据（M6-022：VLAN/站点/用途/负责人）
-- 与 0034 ledger_meta（逐 IP 级）互补：子网级用于全局规划视图与批量默认值。
ALTER TABLE subnet ADD COLUMN IF NOT EXISTS vlan    text NOT NULL DEFAULT '';
ALTER TABLE subnet ADD COLUMN IF NOT EXISTS site    text NOT NULL DEFAULT '';
ALTER TABLE subnet ADD COLUMN IF NOT EXISTS purpose text NOT NULL DEFAULT '';
ALTER TABLE subnet ADD COLUMN IF NOT EXISTS owner   text NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_subnet_vlan ON subnet(vlan) WHERE vlan <> '';
