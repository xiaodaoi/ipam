# observability/ — 监控与告警资产（M6-005）

| 文件 | 用途 |
|---|---|
| `prometheus.yml` | 抓取 control-plane `/metrics`（15s） |
| `alerts.yml` | 控制面/组件掉线、磁盘 85/90%、内存、TLS 到期、5xx 速率、日志外发积压 |
| `alertmanager.yml.example` | 邮件 + Webhook + 钉钉机器人示例 |
| `compose.observability.yaml` | 可选 Prometheus/Alertmanager/Grafana 栈（profile=observability） |
| `grafana/` | 数据源与「IPAM 概览」面板（自动 provision） |

## 启动

```bash
# IPAM 主栈已启动（网络 ipam_default 存在）
cp observability/alertmanager.yml.example observability/alertmanager.yml   # 按需改收件人
# /metrics 令牌（M6-017）：由 deploy/compose/install.sh 自动生成；
# 若未使用 install.sh，请创建该文件并填入与 IPAM_METRICS_TOKEN 相同的值，
# 或在 prometheus.yml 中删除 authorization 段（要求 IPAM 侧未设令牌）。
printf '%s' "$IPAM_METRICS_TOKEN" > observability/metrics_token
GRAFANA_ADMIN_PASSWORD='strong-password' \
docker compose -f observability/compose.observability.yaml --profile observability up -d
```

访问：Prometheus `http://127.0.0.1:9090`、Alertmanager `:9093`、Grafana `:3000`。

## 指标

`GET /metrics`（设置 `IPAM_METRICS_TOKEN` 后需 `Authorization: Bearer <token>`；未设置则对内网开放，建议只在内网/监控网暴露）：

- `ipam_build_info{version}`、`ipam_uptime_seconds`
- `ipam_http_requests_total{method,status}`、`ipam_http_request_duration_seconds_sum/count`
- `ipam_component_up{component="postgres|clickhouse|kea"}`（1/0）
- `ipam_host_cpu_usage_percent`、`ipam_host_cpu_cores`
- `ipam_host_memory_used_bytes/total_bytes`
- `ipam_host_disk_used_percent{mount,device,fstype}`
- `ipam_host_uptime_seconds`
- `ipam_tls_cert_expiry_timestamp_seconds`（配置 TLS 证书时）

## 告警通道

- 邮件：Alertmanager `email_configs`；
- 通用 Webhook：`webhook_configs`（可接内部告警平台）；
- 钉钉/企微：使用机器人 Webhook，按厂商要求配置 body 模板（示例见 `alertmanager.yml.example`）。

## NTP

`deploy/compose/install.sh` 会检查 `timedatectl NTPSynchronized`；未同步时给出警告。生产建议由宿主机 chrony/systemd-timesyncd 统一校时，容器继承宿主时钟。

## 安全

- `/metrics` 与 `/healthz` 位于 RBAC 之前，建议仅对监控网开放；
- 默认只暴露进程/HTTP/组件/宿主聚合指标，不含账号、IP、域名等业务明细。

## 子网利用率

控制面每 5 分钟采样 IPv4 池利用率，暴露：

```
ipam_subnet_utilization_percent{subnet_id="...",subnet="办公",cidr="10.0.0.0/24"} 42.50
```

告警：`IpamSubnetUtilizationHigh`（>85%，15m）、`IpamSubnetUtilizationCritical`（>95%，5m）。

## 日志数据健康

| 指标 | 说明 |
|---|---|
| `ipam_log_rows` | 在线日志行数 |
| `ipam_log_disk_bytes` | 日志磁盘占用 |
| `ipam_log_export_degraded` | 日志采样是否因磁盘水位自动降级（1=是） |
| `ipam_log_parts` | 活跃 part 数 |
| `ipam_log_latest_age_seconds` | 最新日志滞后秒数 |
| `ipam_host_disk_total_bytes` / `free_bytes` | 宿主磁盘总量/可用 |

告警：`IpamLogPartsHigh`、`IpamLogIngestStalled`、`IpamLogDiskGrowthFast`、`IpamLogDiskRatioHigh`。

## 日志外发积压（M6-008）

Vector 通过 `internal_metrics` + `prometheus_exporter` 暴露自身指标（`vector:9598`，由 Prometheus `ipam-vector` job 抓取，不对宿主机发布端口）：

| 指标 | 说明 |
|---|---|
| `vector_buffer_byte_size{component_id}` | 目标 sink 当前缓冲占用字节 |
| `vector_buffer_events{component_id}` | 目标 sink 当前缓冲事件数 |
| `vector_buffer_max_byte_size{component_id}` | 缓冲上限（ClickHouse=1GiB） |
| `vector_component_received_events_total` / `sent_events_total{component_id}` | 收/发事件累计（差值=在途）|
| `vector_component_errors_total{component_kind="sink"}` | 发送错误累计（重试中）|
| `vector_utilization{component_id}` | 组件利用率（0–1）|

告警：

- `IpamLogSinkBacklog`：`component_id=~"clickhouse|siem_.*"` 缓冲占用 >50% 持续 15m（warning）；
- `IpamLogSinkBufferFull`：缓冲占用 >90% 持续 5m（critical，缓冲区为有界 `drop_newest`，再满将丢弃最新日志）；
- `IpamLogSinkErrors`：近 10 分钟 sink 发送错误 >0（warning）。

> 该埋点为所有 sink 通用，不依赖 Syslog/Kafka/Webhook 是否启用；启用外发后 `siem_*` 组件会同样出现在缓冲指标中。

## 授权与终端计量（M6-004B）

| 指标 | 说明 |
|---|---|
| `ipam_license_terminals_used` | 近 24h 活跃终端数（按客户端 MAC 去重，60s 缓存） |
| `ipam_license_terminals_limit` | 授权终端上限（0=不限） |

告警：`IpamLicenseTerminalsOverLimit`（活跃终端数 > 上限持续 6h，仅告警不中断服务）。
未激活/社区版不上报这两项指标，避免把「无授权」误判成「超限」。
