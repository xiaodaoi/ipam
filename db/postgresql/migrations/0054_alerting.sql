-- 0054: 告警治理与降噪（M6-049）
-- 目标：把「日志派生报警 + 全量未确认徽标」升级为可配置的告警治理：
--   记录（ClickHouse）→ 报警档案（alert，带分类/级别/notify 快照）→ 策略/静音/限流 → 通知。
-- 兼容：保留 dedup_key 与现有 API；新增字段均有默认值，历史数据可读。

-- ── 告警策略（数据驱动，不写死）──
CREATE TABLE IF NOT EXISTS alert_policy (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name              text NOT NULL,
  scope_type        text NOT NULL CHECK (scope_type IN
                      ('global','intel_source','blocklist','kind','component','source')),
  scope_key         text NOT NULL DEFAULT '',
  priority          int  NOT NULL DEFAULT 100,
  severity          text NOT NULL DEFAULT 'warning' CHECK (severity IN ('critical','warning','info')),
  notify_mode       text NOT NULL DEFAULT 'notify' CHECK (notify_mode IN ('notify','digest','silent','off')),
  alert_class       text NOT NULL DEFAULT 'policy_block' CHECK (alert_class IN
                      ('security_threat','policy_block','system_health','config_health','')),
  group_by          text NOT NULL DEFAULT 'domain' CHECK (group_by IN ('domain','client','source','none')),
  max_per_hour      int  NOT NULL DEFAULT 0,
  repeat_after_sec  int  NOT NULL DEFAULT 86400,
  resolve_after_sec int  NOT NULL DEFAULT 86400,
  enabled           boolean NOT NULL DEFAULT true,
  created_by        text NOT NULL DEFAULT '',
  updated_by        text NOT NULL DEFAULT '',
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  UNIQUE (scope_type, scope_key)
);
CREATE INDEX IF NOT EXISTS idx_alert_policy_enabled ON alert_policy(enabled, scope_type);

-- ── 静音窗口（匹配条件 JSON：source/kind/component/intelSource/severity/clientIp/domain）──
CREATE TABLE IF NOT EXISTS alert_silence (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  matchers    jsonb NOT NULL DEFAULT '{}'::jsonb,
  starts_at   timestamptz NOT NULL DEFAULT now(),
  ends_at     timestamptz NOT NULL,
  reason      text NOT NULL,
  created_by  text NOT NULL DEFAULT '',
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_alert_silence_window ON alert_silence(starts_at, ends_at);

-- ── 通知通道（密钥落文件，不入库；config 只存非敏感字段）──
CREATE TABLE IF NOT EXISTS notification_channel (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name          text NOT NULL,
  type          text NOT NULL CHECK (type IN
                  ('webhook','email','feishu','dingtalk','wecom','slack','teams')),
  config        jsonb NOT NULL DEFAULT '{}'::jsonb,
  secret_file   text NOT NULL DEFAULT '',
  min_severity  text NOT NULL DEFAULT 'warning' CHECK (min_severity IN ('critical','warning','info')),
  quiet_hours   text NOT NULL DEFAULT '',
  enabled       boolean NOT NULL DEFAULT false,
  last_test_at  timestamptz,
  last_status   text NOT NULL DEFAULT '',
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_notification_channel_enabled ON notification_channel(enabled);

-- ── 投递记录（排障/审计）──
CREATE TABLE IF NOT EXISTS notification_delivery (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  alert_id     uuid NOT NULL,
  channel_id   uuid NOT NULL,
  channel_name text NOT NULL DEFAULT '',
  status       text NOT NULL CHECK (status IN ('sent','failed')),
  attempt      int NOT NULL DEFAULT 1,
  error        text NOT NULL DEFAULT '',
  sent_at      timestamptz NOT NULL DEFAULT now(),
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_notification_delivery_sent ON notification_delivery(sent_at DESC);
CREATE INDEX IF NOT EXISTS idx_notification_delivery_alert ON notification_delivery(alert_id);

-- ── alert 增强 ──
ALTER TABLE alert ADD COLUMN IF NOT EXISTS fingerprint text NOT NULL DEFAULT '';
ALTER TABLE alert ADD COLUMN IF NOT EXISTS alert_class text NOT NULL DEFAULT '';
ALTER TABLE alert ADD COLUMN IF NOT EXISTS notify boolean NOT NULL DEFAULT true;
ALTER TABLE alert ADD COLUMN IF NOT EXISTS policy_id uuid;
ALTER TABLE alert ADD COLUMN IF NOT EXISTS group_key text NOT NULL DEFAULT '';
ALTER TABLE alert ADD COLUMN IF NOT EXISTS window_start timestamptz;
ALTER TABLE alert ADD COLUMN IF NOT EXISTS notify_count int NOT NULL DEFAULT 0;
ALTER TABLE alert ADD COLUMN IF NOT EXISTS last_notified_at timestamptz;
ALTER TABLE alert ADD COLUMN IF NOT EXISTS suppressed_by text NOT NULL DEFAULT '';
ALTER TABLE alert ADD COLUMN IF NOT EXISTS silence_id uuid;
ALTER TABLE alert ADD COLUMN IF NOT EXISTS decision jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE alert ADD COLUMN IF NOT EXISTS repeat_after_sec int NOT NULL DEFAULT 86400;
ALTER TABLE alert ADD COLUMN IF NOT EXISTS resolve_after_sec int NOT NULL DEFAULT 86400;

CREATE UNIQUE INDEX IF NOT EXISTS idx_alert_fingerprint ON alert(fingerprint) WHERE fingerprint <> '';
CREATE INDEX IF NOT EXISTS idx_alert_notify_open ON alert(status, notify, last_seen DESC);

-- 历史数据回填：稳定指纹 + 窗口起点
UPDATE alert SET fingerprint = dedup_key WHERE fingerprint = '';
UPDATE alert SET window_start = date_trunc('hour', first_seen) WHERE window_start IS NULL;

-- 一次性降噪：已归因到 broad 聚合源的历史 open 记录转为归档，不删除、可查。
UPDATE alert
   SET notify = false,
       alert_class = 'policy_block',
       severity = 'info',
       decision = jsonb_build_object('final', 'digest', 'matchedBy', 'migration', 'reason', 'broad source backfill')
 WHERE source = 'dns'
   AND status <> 'resolved'
   AND (intel_source ILIKE '%stevenblack%' OR intel_source ILIKE '%spamhaus%');
