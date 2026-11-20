-- 0048: 报警中心展示情报来源（M6-040 增强）
-- 记录报警发生时命中的外部情报源（可能多个，逗号分隔）：
--   intel_source      = 源 key，便于稳定过滤/审计
--   intel_source_name = 源显示名，历史留痕（源改名/删除后仍可读）
-- 非情报源命中（自定义封禁/内置规则）保持空串，由前端按 category 展示中性文案。

ALTER TABLE alert ADD COLUMN IF NOT EXISTS intel_source text NOT NULL DEFAULT '';
ALTER TABLE alert ADD COLUMN IF NOT EXISTS intel_source_name text NOT NULL DEFAULT '';
