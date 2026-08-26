-- 0006_dns_zones.sql — 本地区域与解析记录（FR-B-03~05、§13.4 解析记录）
CREATE TABLE IF NOT EXISTS dns_zone (
  id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name      text NOT NULL UNIQUE,       -- FQDN 如 corp.local.
  kind      text NOT NULL DEFAULT 'auth' CHECK (kind IN ('auth','local')),
  enabled   boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS dns_record (
  id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  zone_id   uuid NOT NULL REFERENCES dns_zone(id) ON DELETE CASCADE,
  name      text NOT NULL,              -- 相对名（如 www）或 FQDN
  rec_type  text NOT NULL CHECK (rec_type IN ('A','AAAA','CNAME','PTR')),
  ttl       int NOT NULL DEFAULT 300,
  rdata     text NOT NULL,
  enabled   boolean NOT NULL DEFAULT true,
  UNIQUE (zone_id, name, rec_type)
);
CREATE INDEX IF NOT EXISTS idx_record_zone ON dns_record(zone_id);
