-- 0020: org_group 加排序字段（组织拖拽排序）。
ALTER TABLE org_group ADD COLUMN IF NOT EXISTS sort_order integer NOT NULL DEFAULT 0;
