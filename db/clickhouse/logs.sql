-- logs 宽表（§3 ClickHouse 示意落地；TTL 按容量策略 180 天）
-- client_ip/sip 为 Nullable：DNS 行无 client_ip、DHCP 行无 sip，'' 无法解析为 IPv6 会整批失败
CREATE DATABASE IF NOT EXISTS ipam;

CREATE TABLE IF NOT EXISTS ipam.logs
(
    ts          DateTime64(3),
    type        LowCardinality(String),   -- dhcp | dns
    severity    LowCardinality(String),
    client_mac  String,
    client_ip   Nullable(IPv6),
    sip         Nullable(IPv6),
    domain      String,
    rcode       LowCardinality(String),
    action      LowCardinality(String),   -- resolve / blocked / lease_commit ...
    category    LowCardinality(String),   -- 命中封禁时的分类（FR-B-18）
    detail      String,
    answer_ip   Nullable(IPv6),            -- DNS 应答 IP（log-replies 解析；A/AAAA 最终地址）
    INDEX idx_domain domain TYPE tokenbf_v1(10240, 3, 0) GRANULARITY 4
)
ENGINE = MergeTree
PARTITION BY toDate(ts)
ORDER BY (type, client_mac, ts)
TTL toDateTime(ts) + INTERVAL 180 DAY DELETE;

-- TopN 物化视图（§8 预算支撑）：M4-002 当前走原表聚合已达标，
-- 数据量级上升（DNS 事件带源 IP 后口径变宽）按 §6 触发条件切换查询路径。
CREATE MATERIALIZED VIEW IF NOT EXISTS ipam.logs_topn_hourly
ENGINE = SummingMergeTree
PARTITION BY toDate(hour)
ORDER BY (type, hour, domain, client_mac)
TTL hour + INTERVAL 180 DAY DELETE
AS SELECT
    toStartOfHour(ts) AS hour,
    type,
    domain,
    client_mac,
    count() AS cnt
FROM ipam.logs
GROUP BY hour, type, domain, client_mac;