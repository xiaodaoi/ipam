-- 0023: 子网 CIDR 全局唯一（Kea 要求 prefix 唯一；重复会导致全量 config-set 被拒）。
DELETE FROM subnet a USING subnet b WHERE a.id <> b.id AND a.cidr = b.cidr;
CREATE UNIQUE INDEX IF NOT EXISTS uniq_subnet_cidr ON subnet (cidr);
