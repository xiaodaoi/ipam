-- 0042: 组织名称规范化与同父唯一（M6-026）
-- 背景：旧约束 UNIQUE (parent_id, name) 在 parent_id IS NULL（根节点）时失效
--       （PostgreSQL 中 NULL 互不相等），且大小写敏感，与内存实现 / 服务层判重不一致。
-- 处理：1) 清理空名称脏数据（仅限无子节点且无引用的节点）；
--       2) 去除现存名称首尾空白；
--       3) 用表达式唯一索引统一根节点与子节点的判重语义。

-- 1) 历史空名称节点：仅删除完全无引用的叶子节点，避免误删业务数据。
DELETE FROM org_group g
 WHERE btrim(g.name) = ''
   AND NOT EXISTS (SELECT 1 FROM org_group c WHERE c.parent_id = g.id)
   AND NOT EXISTS (SELECT 1 FROM subnet s WHERE s.org_id = g.id)
   AND NOT EXISTS (SELECT 1 FROM asset a WHERE a.org_id = g.id);

-- 2) 名称首尾空白规范化（插入/改名服务层已 trim，此处修复历史数据）。
UPDATE org_group SET name = btrim(name) WHERE name <> btrim(name);

-- 3) 旧的同父唯一约束对根节点无效，替换为表达式唯一索引。
ALTER TABLE org_group DROP CONSTRAINT IF EXISTS org_group_parent_id_name_key;

CREATE UNIQUE INDEX IF NOT EXISTS uniq_org_parent_name
  ON org_group (COALESCE(parent_id, '00000000-0000-0000-0000-000000000000'::uuid), lower(name));
