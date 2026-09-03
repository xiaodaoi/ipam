-- 0019: reservation 表补 ipv4 唯一约束。
-- 背景：无唯一约束时 Upsert(INSERT ... ON CONFLICT DO NOTHING) 从不冲突，
-- 同地址重复绑定会积累重复行；且 ON CONFLICT 目标缺位使"改绑"语义无法落库。
-- 步骤：先去重（保留每组最小 id），再建部分唯一索引（ipv4 可空，保留/绑定均为 v4 单址）。

DELETE FROM reservation a
USING reservation b
WHERE a.ipv4 IS NOT NULL
  AND b.ipv4 IS NOT NULL
  AND a.ipv4 = b.ipv4
  AND a.id > b.id;

CREATE UNIQUE INDEX IF NOT EXISTS uniq_reservation_ipv4
  ON reservation (ipv4) WHERE ipv4 IS NOT NULL;
