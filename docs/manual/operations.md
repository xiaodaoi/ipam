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

## 日志容量与长期运行

关键指标：

| 指标 | 含义 | 建议 |
|---|---|---|
| `ipam_log_rows` | 在线日志行数 | 结合 TTL 评估 |
| `ipam_log_disk_bytes` | 日志磁盘占用 | 占磁盘 >60% 告警 |
| `ipam_log_parts` | 活跃 part 数 | >500 告警，检查写入/合并 |
| `ipam_log_latest_age_seconds` | 最新日志滞后 | >600s 告警，查 Vector |
| `ipam_host_disk_free_bytes` | 宿主可用空间 | 提前扩容 |

建议：

- 在线保留 180–365 天；多年合规留存用按月 Parquet 归档；
- Vector 使用 1GB 有界磁盘缓冲，CH 故障超过缓冲会丢弃最新日志（优先保宿主稳定）；
- 查询超时先缩小时间窗/加 type/domain 过滤；重复分页有 15s total 缓存；
- 翻页超过 1 万条会返回 `PAGE_TOO_DEEP`，请缩小时间窗/增过滤条件或导出 CSV（避免深 offset 扫描）；
- 不要把 `read_from` 长期设为 `beginning`。

## 日志查询慢 / “日志存储暂不可达”

先区分“超时”与“存储宕机”（接口 code）：

- `QUERY_TIMEOUT`：查询超过 3s 预算，缩小时间范围或等待清理；
- `CH_DOWN`：ClickHouse 不可达/其他错误，查看 control-plane 日志 `[logs] 查询失败`。

排查步骤：

```bash
# 行数/时间范围/磁盘
docker compose exec clickhouse clickhouse-client --query \
  "SELECT count(), min(ts), max(ts) FROM ipam.logs"
docker compose exec clickhouse clickhouse-client --query \
  "SELECT table, formatReadableSize(sum(bytes_on_disk)), sum(rows) \
     FROM system.parts WHERE database='ipam' AND active GROUP BY table"

# Vector checkpoint 是否持久化（应为 vector-data 卷）
docker inspect -f '{{range .Mounts}}{{.Name}}->{{.Destination}} {{end}}' ipam-vector-1

# 每分钟入库量，异常洪峰通常 >10 万/分钟
docker compose exec clickhouse clickhouse-client --query \
  "SELECT toStartOfMinute(ts) m, count() FROM ipam.logs \
     WHERE ts >= now() - INTERVAL 2 HOUR GROUP BY m ORDER BY m DESC LIMIT 20"
```

历史表结构升级（`ORDER BY` 改为 `ts` 优先）：

```bash
scripts/migrate-ch-logs.sh --yes
# 确认后：DROP TABLE ipam.logs_old
```

> 不要将 Vector file source 的 `read_from` 长期设为 `beginning`；checkpoint 丢失会导致整文件重放。

## LDAP / AD 目录登录（M6-007B）

入口：系统管理 → 目录服务。

1. 填写服务器地址（`ldap://`/`ldaps://`）、Base DN、用户过滤器；
2. 目录不允许匿名搜索时填写服务账号 DN 与口令（口令也可用 `IPAM_LDAP_BIND_PASSWORD` 注入，建议生产使用该方式）；
3. 配置组搜索 Base DN/过滤器与「组 → 角色」映射；
4. 保存后用「连通性测试」验证一次真实 bind，再启用。

常用过滤器：

| 目录 | 用户过滤器 | 组过滤器 |
|---|---|---|
| OpenLDAP | `(uid={username})` | `(&(objectClass=groupOfNames)(member={dn}))` |
| AD | `(sAMAccountName={username})` | `(&(objectClass=group)(member={dn}))` |

行为说明：

- 目录账号首次登录自动创建本地影子账号（`authSource=ldap`），角色按组映射，未匹配用默认角色；
- 口令每次登录实时 bind，平台不保存目录口令；本地口令为随机值，不能使用；
- 目录账号不能在本平台改密/重置口令，需在目录服务操作；
- 修改组映射后，用户下次登录自动同步角色并吊销旧会话；
- 目录不可达时，已绑定目录账号返回 503 `LDAP_UNAVAILABLE`，本地账号不受影响；
- 根 CA 不在系统信任链时，可临时开启「跳过证书校验」排查，生产请安装 CA。

## 账号恢复与 TOTP 恢复码

- 用户可在「个人中心 → 安全设置」生成 10 个一次性恢复码，登录时替代动态口令；
- 恢复码仅存散列、明文仅展示一次，请离线保存；重新生成后旧码全部失效；
- 恢复码用尽且认证器丢失时，由管理员在「账号管理 → 重置 2FA」清除绑定与恢复码（旧会话立即失效，用户重新登录后重新绑定）；
- 目录账号也可生成恢复码（确认身份时使用动态口令）。

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
