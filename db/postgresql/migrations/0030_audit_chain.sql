-- 0030: 审计完整性（M6-006）——请求体脱敏 + SHA256 链哈希
ALTER TABLE operation_audit ADD COLUMN IF NOT EXISTS payload text NOT NULL DEFAULT '';
ALTER TABLE operation_audit ADD COLUMN IF NOT EXISTS prev_hash text NOT NULL DEFAULT '';
ALTER TABLE operation_audit ADD COLUMN IF NOT EXISTS chain_hash text NOT NULL DEFAULT '';
