-- 0047: 应用内报警中心（M6-040）
-- 报警 = 聚合结果，不复制日志明细：DNS 风险访问从 ClickHouse action=blocked 聚合，
-- 系统故障来自健康探针。dedup_key 保证重复采集不重复建行。

CREATE TABLE IF NOT EXISTS alert (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  dedup_key    text NOT NULL UNIQUE,              -- dns|<ip>|<domain>|<YYYYMMDDHH> | system|<kind>|<component>
  source       text NOT NULL CHECK (source IN ('dns','system')),
  kind         text NOT NULL,                     -- risk_url | db_down | logstore_down | ...
  severity     text NOT NULL DEFAULT 'warning' CHECK (severity IN ('critical','warning','info')),
  status       text NOT NULL DEFAULT 'open' CHECK (status IN ('open','acked','resolved')),
  title        text NOT NULL,
  message      text NOT NULL DEFAULT '',
  client_ip    text NOT NULL DEFAULT '',
  client_mac   text NOT NULL DEFAULT '',
  domain       text NOT NULL DEFAULT '',
  component    text NOT NULL DEFAULT '',
  count        bigint NOT NULL DEFAULT 0,
  detail       text NOT NULL DEFAULT '',
  first_seen   timestamptz NOT NULL DEFAULT now(),
  last_seen    timestamptz NOT NULL DEFAULT now(),
  acked_at     timestamptz,
  acked_by     text NOT NULL DEFAULT '',
  resolved_at  timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_alert_status_seen ON alert(status, last_seen DESC);
CREATE INDEX IF NOT EXISTS idx_alert_source_seen ON alert(source, last_seen DESC);
CREATE INDEX IF NOT EXISTS idx_alert_seen ON alert(last_seen DESC);
