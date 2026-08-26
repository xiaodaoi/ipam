-- 0005_forward_rules.sql — 条件转发规则（FR-B-02、§13.4 转发规则）
CREATE TABLE IF NOT EXISTS forward_rule (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  domain       text NOT NULL UNIQUE,   -- 域名后缀（含 "." 根，如 corp.local. / .）
  upstream_ids uuid[] NOT NULL DEFAULT '{}',
  enabled      boolean NOT NULL DEFAULT true,
  note         text,
  created_at   timestamptz NOT NULL DEFAULT now()
);
