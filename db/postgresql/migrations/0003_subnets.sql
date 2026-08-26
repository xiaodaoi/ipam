-- 0003_subnets.sql — 子网与地址池（FR-C、§13.4 组织关联链）
CREATE TABLE IF NOT EXISTS subnet (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id      uuid REFERENCES org_group(id) ON DELETE RESTRICT,
  name        text NOT NULL,
  family      smallint NOT NULL CHECK (family IN (4,6)),
  cidr        cidr NOT NULL,
  kea_subnet_id int,
  description text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id, name)
);
CREATE INDEX IF NOT EXISTS idx_subnet_org ON subnet(org_id);

CREATE TABLE IF NOT EXISTS address_pool (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  subnet_id   uuid NOT NULL REFERENCES subnet(id) ON DELETE CASCADE,
  family      smallint NOT NULL CHECK (family IN (4,6)),
  start_addr  inet NOT NULL,
  end_addr    inet NOT NULL,
  kind        text NOT NULL DEFAULT 'dynamic'
              CHECK (kind IN ('dynamic','pd','excluded')),
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_pool_subnet ON address_pool(subnet_id);