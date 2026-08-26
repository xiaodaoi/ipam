-- 0004_upstreams.sql — DNS 上游服务器（FR-B-01、§13.4 上游管理）
CREATE TABLE IF NOT EXISTS upstream (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name       text NOT NULL,
  addrs      text[] NOT NULL,          -- 上游地址列表（v4/v6，如 223.5.5.5:53 / 2400:3200::1）
  protocol   text NOT NULL DEFAULT 'udp' CHECK (protocol IN ('udp','tcp','dot')),
  weight     int  NOT NULL DEFAULT 1,
  enabled    boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_upstream_enabled ON upstream(enabled);