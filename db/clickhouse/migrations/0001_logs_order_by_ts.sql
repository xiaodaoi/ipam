-- 0001: logs 表 ORDER BY 调整为 ts 优先（M6-014 日志查询性能修复）
-- 适用：已有部署（db/clickhouse/logs.sql 仅初始化新库）。
-- 步骤：创建 v2 → 去重/搬迁 → 原子切换 → 重建 TopN 物化视图。
-- 注意：大表请在维护窗口执行；先备份/确认磁盘余量（约需 1.2 倍表大小）。

CREATE TABLE IF NOT EXISTS ipam.logs_v2
(
    ts          DateTime64(3),
    type        LowCardinality(String),
    severity    LowCardinality(String),
    client_mac  String,
    client_ip   Nullable(IPv6),
    sip         Nullable(IPv6),
    domain      String,
    rcode       LowCardinality(String),
    action      LowCardinality(String),
    category    LowCardinality(String),
    detail      String,
    answer_ip   Nullable(IPv6),
    INDEX idx_domain domain TYPE tokenbf_v1(10240, 3, 0) GRANULARITY 4
)
ENGINE = MergeTree
PARTITION BY toDate(ts)
ORDER BY (ts, client_mac, domain)
TTL toDateTime(ts) + INTERVAL 180 DAY DELETE;

-- 去重搬迁（如数据量大，可按 toDate(ts) 分批执行）
INSERT INTO ipam.logs_v2
SELECT DISTINCT * FROM ipam.logs;

-- 校验行数后再切换（人工确认）
-- SELECT count() FROM ipam.logs; SELECT count() FROM ipam.logs_v2;

RENAME TABLE ipam.logs TO ipam.logs_old, ipam.logs_v2 TO ipam.logs;

-- 重建 TopN 物化视图数据（避免旧表重复数据残留）
TRUNCATE TABLE ipam.logs_topn_hourly;
INSERT INTO ipam.logs_topn_hourly
SELECT toStartOfHour(ts) AS hour, type, domain, client_mac, count() AS cnt
FROM ipam.logs
GROUP BY hour, type, domain, client_mac;

-- 确认无误后删除旧表：
-- DROP TABLE ipam.logs_old;
