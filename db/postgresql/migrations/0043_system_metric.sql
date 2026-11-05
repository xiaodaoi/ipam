-- 0043: 主机与控制面指标采样（M6-032）
-- 背景：仪表盘/运行页只有瞬时快照，重启即丢，无法回答「昨晚 2 点是不是打满」。
-- 设计：60s 采样、保留 30 天（服务层 Prune），标量进主表、挂载点明细进子表。
-- 注意：备份时用 pg_dump --exclude-table-data 排除这两张表的数据行（结构保留），
--       避免换机器恢复后历史里出现「上一台机器」的指标。

CREATE TABLE IF NOT EXISTS system_metric (
  id                 bigserial PRIMARY KEY,
  node               text NOT NULL DEFAULT 'local',  -- 为 M6-003B 双机预留
  ts                 timestamptz NOT NULL,
  -- 宿主
  cpu_pct            numeric(6,2),
  cores              int,
  load1              real,
  load5              real,
  load15             real,
  mem_total_bytes    bigint,
  mem_used_bytes     bigint,
  mem_pct            numeric(6,2),
  uptime_sec         numeric(14,2),
  disk_root_pct      numeric(6,2),
  disk_max_pct       numeric(6,2),
  disk_worst_mount   text,
  -- 控制面（用于定位「页面卡/查询慢」，与宿主指标同一时间轴）
  goroutines         int,
  heap_bytes         bigint,
  db_pool_in_use     int,
  db_pool_max        int,
  log_latest_age_sec bigint,   -- 最新一条日志距今秒数；-1 = 无数据或不可用
  CONSTRAINT uniq_system_metric_node_ts UNIQUE (node, ts)
);

CREATE INDEX IF NOT EXISTS idx_system_metric_node_ts ON system_metric(node, ts DESC);
CREATE INDEX IF NOT EXISTS idx_system_metric_ts ON system_metric(ts);

CREATE TABLE IF NOT EXISTS system_disk_metric (
  id          bigserial PRIMARY KEY,
  metric_id   bigint NOT NULL REFERENCES system_metric(id) ON DELETE CASCADE,
  mount       text NOT NULL,
  device      text,
  fstype      text,
  total_bytes bigint,
  used_bytes  bigint,
  used_pct    numeric(6,2)
);

CREATE INDEX IF NOT EXISTS idx_system_disk_metric_metric ON system_disk_metric(metric_id);
