-- 0029: 备份与恢复（M6-002）
CREATE TABLE IF NOT EXISTS backup_record (
  id          uuid PRIMARY KEY,
  kind        text NOT NULL CHECK (kind IN ('manual','auto','pre_upgrade')),
  status      text NOT NULL CHECK (status IN ('running','success','failed','restoring')),
  note        text NOT NULL DEFAULT '',
  dir         text NOT NULL DEFAULT '',
  size_bytes  bigint NOT NULL DEFAULT 0,
  checksum    text NOT NULL DEFAULT '',
  months      text[] NOT NULL DEFAULT '{}',
  error       text NOT NULL DEFAULT '',
  started_at  timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz
);
CREATE INDEX IF NOT EXISTS idx_backup_record_started ON backup_record(started_at DESC);

CREATE TABLE IF NOT EXISTS backup_settings (
  id              smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  enabled         boolean NOT NULL DEFAULT true,
  schedule_hour   int NOT NULL DEFAULT 3 CHECK (schedule_hour BETWEEN 0 AND 23),
  retention_count int NOT NULL DEFAULT 7 CHECK (retention_count BETWEEN 1 AND 90),
  updated_at      timestamptz NOT NULL DEFAULT now()
);
INSERT INTO backup_settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
