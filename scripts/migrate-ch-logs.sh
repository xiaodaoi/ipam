#!/usr/bin/env bash
# 重建 ClickHouse logs 表：ts 优先排序 + 去重（M6-014）。
# 用法：scripts/migrate-ch-logs.sh [--compose-dir deploy/compose] [--yes]
# 说明：需在维护窗口执行；脚本只做 v2 建表/搬迁/切换，旧表保留为 logs_old 供回滚。
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
compose_dir="$root/deploy/compose"
yes=0
while [ $# -gt 0 ]; do
  case "$1" in
    --compose-dir) compose_dir="$2"; shift 2 ;;
    --yes) yes=1; shift ;;
    *) echo "未知参数: $1"; exit 2 ;;
  esac
done
cd "$compose_dir"
if [ "$yes" -ne 1 ]; then
  echo "将重建日志表（约需 1.2 倍表空间），并保留旧表 logs_old。输入 yes 继续："
  read -r ans
  [ "$ans" = "yes" ] || { echo "已取消"; exit 1; }
fi
docker compose exec -T clickhouse clickhouse-client --multiquery < "$root/db/clickhouse/migrations/0001_logs_order_by_ts.sql"
echo "完成：logs 已切换为 (ts,type,client_mac) 排序；确认后手动 DROP TABLE ipam.logs_old"
