# 运维手册

## 健康与监控

| 端点 | 用途 | 认证 |
|---|---|---|
| `/healthz` | 进程存活 | 否 |
| `/readyz` | PostgreSQL 就绪 | 否 |
| `/metrics` | Prometheus 指标 | 否（仅内网） |
| `/system/info` | 版本/就绪 | Bearer |
| `/system/metrics` | 宿主 CPU/内存/磁盘 | Bearer |

监控栈：

```bash
GRAFANA_ADMIN_PASSWORD='...' \
docker compose -f observability/compose.observability.yaml --profile observability up -d
```

告警规则/通道见 `observability/`。

## 备份

- 页面：系统管理 → 备份与恢复；
- 内容：PostgreSQL custom dump + 配置 tar + ClickHouse 日志按月 Parquet；
- 自动：每日 UTC 指定小时，保留 N 份；
- 下载：页面“下载”或 `GET /system/backups/{id}/download`。

## 恢复

1. 维护窗口；
2. 页面选择“恢复”并确认；
3. 恢复顺序：PG `pg_restore --clean` → 清空日志表 → 回放 Parquet；
4. 完成后刷新页面，检查账号/子网/租约。

## 日志外发

日志中心 → 外发与采样：

- Syslog RFC5424（TCP/UDP）、Kafka、Webhook；
- 全量/1/N/仅异常；
- 保存后 Vector `--watch-config` 热加载；
- 磁盘 80% 预警、90% 建议人工降级（自动降级待后续）。

## 审计

- 操作审计可在日志中心 → 操作审计查询/导出；
- “完整性校验”重算 SHA256 链，异常会给出 brokenId；
- 保留 180 天，自动清理。

## 故障处理

| 现象 | 排查 |
|---|---|
| 页面 502/不可达 | `docker compose ps`、`logs control-plane` |
| 登录 401 | 口令/账号状态；看 audit `login` 记录 |
| DNS 不解析 | `dig @<host> ...`；unbound 日志；上游健康 |
| DHCP 不下发 | Kea 日志；子网/池配置；中继 giaddr/option82 |
| 日志无数据 | Vector 状态；ClickHouse 写入；`/metrics` 组件 up |

## 单机韧性（systemd + 看门狗）

```bash
sudo bash scripts/install-systemd.sh
```

- `ipam.service`：开机 `docker compose up -d --wait`；
- `ipam-watchdog.timer`：每分钟检查 `/healthz`，失败自动拉起；容器 unhealthy 自动 restart；
- 不等同于 HA：宿主断电/内核/磁盘故障仍不可用。

## 单机 HA 边界

单机不提供宿主故障自动接管；需要双机 HA 时按 `M6-003B` 增配第二节点（keepalived + Kea HA + config-replicate）。
