-- 0038: 审计实体级变更前快照（M6-006）——记录变更前实体（脱敏 JSON），与脱敏请求体配对形成 before/after
ALTER TABLE operation_audit ADD COLUMN IF NOT EXISTS before_json text NOT NULL DEFAULT '';
