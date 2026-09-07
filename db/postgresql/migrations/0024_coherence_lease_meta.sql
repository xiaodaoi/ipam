-- 0024: coherence_binding 补租约元数据（真实租用/到期时间展示与状态判定数据源）。
ALTER TABLE coherence_binding ADD COLUMN IF NOT EXISTS cltt bigint NOT NULL DEFAULT 0;
ALTER TABLE coherence_binding ADD COLUMN IF NOT EXISTS valid_lft integer NOT NULL DEFAULT 0;
