-- 0040: DNS 能力补齐（M6-023）
-- 1) DoT 上游 TLS SNI；2) secondary（AXFR）区域主服务器；3) kind 放开 secondary。

ALTER TABLE upstream ADD COLUMN IF NOT EXISTS tls_name text NOT NULL DEFAULT '';

ALTER TABLE dns_zone ADD COLUMN IF NOT EXISTS primary_servers text NOT NULL DEFAULT '';

ALTER TABLE dns_zone DROP CONSTRAINT IF EXISTS dns_zone_kind_check;
ALTER TABLE dns_zone ADD CONSTRAINT dns_zone_kind_check
  CHECK (kind IN ('auth','local','secondary'));
