-- logs 宽表（§3 ClickHouse 示意落地；TTL 按容量策略 180 天）
CREATE DATABASE IF NOT EXISTS ipam;

CREATE TABLE IF NOT EXISTS ipam.logs
(
    ts          DateTime64(3),
    type        LowCardinality(String),   -- dhcp | dns
    severity    LowCardinality(String),
    client_mac  String,
    client_ip   IPv6,
    sip         IPv6,
    domain      String,
    rcode       LowCardinality(String),
    action      LowCardinality(String),   -- resolve / blocked / lease_commit ...
    category    LowCardinality(String),   -- 命中封禁时的分类（FR-B-18）
    detail      String,
    INDEX idx_domain domain TYPE tokenbf_v1(10240) GRANULARITY 4
)
ENGINE = MergeTree
PARTITION BY toDate(ts)
ORDER BY (type, client_mac, ts)
TTL toDateTime(ts) + INTERVAL 180 DAY DELETE;