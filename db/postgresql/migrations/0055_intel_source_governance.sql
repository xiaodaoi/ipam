-- 0055: 情报源运营闭环（M6-039 P2：质量闸门 + 版本回滚 + 同步观测）
-- 原则不变：IOC 仍以磁盘缓存为准（百万级不入库），这里只存元数据、版本指针与执行记录。

ALTER TABLE intel_source
  -- 单源在所在层的贡献上限（百分比）；0=不限；仅同层多个源竞争容量时生效。
  ADD COLUMN IF NOT EXISTS max_share_pct int NOT NULL DEFAULT 60,
  -- 最近一次同步的观测值（UI 直接展示，避免再查 run 表）。
  ADD COLUMN IF NOT EXISTS last_duration_ms int NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS last_added int NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS last_removed int NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS last_excluded int NOT NULL DEFAULT 0;

-- 版本快照：每次成功同步且内容变化时生成；文件位于 <rpz>/sources/<key>/rev-<n>.domains。
CREATE TABLE IF NOT EXISTS intel_source_revision (
  id          bigserial PRIMARY KEY,
  source_id   uuid NOT NULL REFERENCES intel_source(id) ON DELETE CASCADE,
  revision    int  NOT NULL,
  entries     int  NOT NULL,
  path        text NOT NULL,
  sha256      text NOT NULL DEFAULT '',
  created_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (source_id, revision)
);
CREATE INDEX IF NOT EXISTS idx_intel_source_revision_source
  ON intel_source_revision(source_id, revision DESC);

-- 执行记录：手动/定时/启用/回滚各一次，用于排障与指标。
CREATE TABLE IF NOT EXISTS intel_source_run (
  id           bigserial PRIMARY KEY,
  source_id    uuid NOT NULL REFERENCES intel_source(id) ON DELETE CASCADE,
  started_at   timestamptz NOT NULL,
  duration_ms  int NOT NULL DEFAULT 0,
  status       text NOT NULL CHECK (status IN ('ok','failed','rolled_back')),
  trigger      text NOT NULL DEFAULT 'manual' CHECK (trigger IN ('manual','schedule','enable','rollback')),
  entries      int NOT NULL DEFAULT 0,
  added        int NOT NULL DEFAULT 0,
  removed      int NOT NULL DEFAULT 0,
  excluded     int NOT NULL DEFAULT 0,
  revision     int NOT NULL DEFAULT 0,
  error        text NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_intel_source_run_source
  ON intel_source_run(source_id, started_at DESC);
