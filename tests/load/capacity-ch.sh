#!/usr/bin/env bash
# ClickHouse 日志容量与查询压测（M6-016）。
#
# 目的：在真实 ClickHouse 容器上生成可控规模的日志表，测「单日 500 万行」口径的
# 写入吞吐、压缩后体积、典型查询延迟，为容量规划（保留天数/磁盘）提供数据。
#
# 用法：
#   bash tests/load/capacity-ch.sh                 # 默认 500 万行（约 1 天量）
#   ROWS=20000000 bash tests/load/capacity-ch.sh   # 约 4 天量
#
# 依赖：docker、正在运行的 ipam ClickHouse 容器（默认 ipam-clickhouse-1）。
# 说明：脚本只创建/删除 `logs_capacity` 影子表，不触碰生产表 ipam.logs。
set -euo pipefail

CH_CONTAINER=${CH_CONTAINER:-ipam-clickhouse-1}
CH_DB=${CH_DB:-ipam}
ROWS=${ROWS:-5000000}
TABLE=${TABLE:-logs_capacity}
OUT_DIR=${OUT_DIR:-tests/load/artifacts}
STAMP=$(date +%Y%m%d-%H%M%S)
OUT_FILE="$OUT_DIR/capacity-${ROWS}-${STAMP}.txt"

mkdir -p "$OUT_DIR"

ch() { docker exec -i "$CH_CONTAINER" clickhouse-client --database "$CH_DB" "$@"; }

# qtime 标签 SQL：单次执行并把 clickhouse-client 自报耗时（秒）打印出来。
qtime() {
  local label=$1 sql=$2 secs
  secs=$(ch --time -q "$sql" 2>&1 | tail -n 1)
  printf '%-34s %ss\n' "$label" "$secs"
}

main() {
  {
    echo "# ClickHouse 容量压测 $STAMP"
    echo "容器=$CH_CONTAINER 库=$CH_DB 表=$TABLE 行数=$ROWS"
    echo

    echo "== 建表 =="
    ch -q "DROP TABLE IF EXISTS ${TABLE}"
    ch -q "CREATE TABLE ${TABLE} AS ${CH_DB}.logs"

    echo "== 写入（单日样本：80% dns / 20% dhcp，2000 客户端 / 5 万域名）=="
    local insert_secs
    insert_secs=$(ch --time -q "
INSERT INTO ${TABLE}
SELECT
  toDateTime64('2026-10-31 00:00:00', 3) + toIntervalMillisecond(number % 86400000) AS ts,
  if(number % 10 < 8, 'dns', 'dhcp') AS type,
  if(number % 100 = 0, 'WARN', 'INFO') AS severity,
  if(number % 10 < 8, '', concat(
    'aa:bb:cc:',
    substring(lower(hex(number % 2000 + 1)), -2, 2), ':',
    substring(lower(hex((number * 7) % 2000 + 1)), -2, 2), ':',
    substring(lower(hex((number * 13) % 2000 + 1)), -2, 2))) AS client_mac,
  toNullable(toIPv6(concat('2001:db8::', lower(hex(number % 2000 + 16))))) AS client_ip,
  toNullable(toIPv6(concat('2001:db8:1::', lower(hex(number % 2000 + 16))))) AS sip,
  concat('host-', toString(number % 50000), '.corp.local.') AS domain,
  if(number % 100 = 0, 'NXDOMAIN', 'NOERROR') AS rcode,
  if(number % 10 < 8, 'dns_query', 'lease_commit') AS action,
  if(number % 10 < 8, 'allowed', 'dhcp') AS category,
  concat('[', toString(number), '] unbound info: query ', toString(number % 50000), '.corp.local. A IN') AS detail,
  toNullable(toIPv6(concat('2001:db8::', lower(hex(number % 2000 + 16))))) AS answer_ip
FROM numbers(${ROWS})" 2>&1 | tail -n 1)
    local rps
    rps=$(awk -v n="$ROWS" -v s="$insert_secs" 'BEGIN { if (s+0 > 0) printf "%.0f", n/s; else print "n/a" }')
    printf '%-34s %ss（约 %s 行/秒，单连接单批）\n' "INSERT ${ROWS} 行" "$insert_secs" "$rps"
    echo

    echo "== 规模（system.parts，含索引/标记）=="
    ch -q "
SELECT
  sum(rows) AS total_rows,
  formatReadableSize(sum(bytes_on_disk)) AS disk,
  formatReadableSize(sum(data_compressed_bytes)) AS compressed,
  formatReadableSize(sum(data_uncompressed_bytes)) AS uncompressed,
  round(sum(data_uncompressed_bytes) / greatest(sum(data_compressed_bytes), 1), 2) AS ratio,
  round(sum(bytes_on_disk) / greatest(sum(rows), 1), 1) AS bytes_per_row
FROM system.parts WHERE database='${CH_DB}' AND table='${TABLE}' AND active
FORMAT Vertical"
    ch -q "
SELECT partition, count() AS parts, sum(rows) AS rows, formatReadableSize(sum(bytes_on_disk)) AS disk
FROM system.parts WHERE database='${CH_DB}' AND table='${TABLE}' AND active
GROUP BY partition ORDER BY partition FORMAT PrettyCompactMonoBlock"
    echo

    echo "== 查询延迟（单次；客户端自报耗时）=="
    local d0="2026-10-31 00:00:00" d1="2026-10-31 23:59:59"
    qtime "count 全天 type=dns" \
      "SELECT count() FROM ${TABLE} WHERE type='dns' AND ts >= '${d0}' AND ts <= '${d1}'"
    qtime "页面首页 50 行（全列）" \
      "SELECT ts, type, severity, client_mac, coalesce(toString(client_ip),''), coalesce(toString(sip),''), domain, rcode, action, category, coalesce(toString(answer_ip),''), detail FROM ${TABLE} WHERE ts >= '${d0}' AND ts <= '${d1}' ORDER BY ts DESC, client_mac DESC, domain DESC LIMIT 50"
    qtime "域名子串检索 全天" \
      "SELECT count() FROM ${TABLE} WHERE ts >= '${d0}' AND ts <= '${d1}' AND domain LIKE '%host-12345%'"
    qtime "客户端 Top10 24h（dhcp）" \
      "SELECT client_mac, count() AS c FROM ${TABLE} WHERE type='dhcp' AND ts >= '${d0}' AND ts <= '${d1}' GROUP BY client_mac ORDER BY c DESC LIMIT 10"
    echo

    echo "== 清理 =="
    ch -q "DROP TABLE IF EXISTS ${TABLE}"
    echo "done"
  } 2>&1 | tee "$OUT_FILE"
  echo
  echo "结果已保存: $OUT_FILE"
}

main "$@"
