-- 0032: 日志外发与采样策略（M6-008）
CREATE TABLE IF NOT EXISTS log_export_settings (
  id              smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  syslog_enabled  boolean NOT NULL DEFAULT false,
  syslog_address  text NOT NULL DEFAULT '',
  syslog_mode     text NOT NULL DEFAULT 'tcp' CHECK (syslog_mode IN ('tcp','udp')),
  kafka_enabled   boolean NOT NULL DEFAULT false,
  kafka_brokers   text NOT NULL DEFAULT '',
  kafka_topic     text NOT NULL DEFAULT '',
  webhook_enabled boolean NOT NULL DEFAULT false,
  webhook_url     text NOT NULL DEFAULT '',
  sampling_mode   text NOT NULL DEFAULT 'all' CHECK (sampling_mode IN ('all','sample','errors')),
  sample_rate     int NOT NULL DEFAULT 10 CHECK (sample_rate BETWEEN 1 AND 1000),
  updated_at      timestamptz NOT NULL DEFAULT now()
);
INSERT INTO log_export_settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
