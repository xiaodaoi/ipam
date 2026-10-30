# observability/ — 监控与告警资产（M6-005）

| 文件 | 用途 |
|---|---|
| `prometheus.yml` | 抓取 control-plane `/metrics`（15s） |
| `alerts.yml` | 控制面/组件掉线、磁盘 85/90%、内存、TLS 到期、5xx 速率 |
| `alertmanager.yml.example` | 邮件 + Webhook + 钉钉机器人示例 |
| `compose.observability.yaml` | 可选 Prometheus/Alertmanager/Grafana 栈（profile=observability） |
| `grafana/` | 数据源与「IPAM 概览」面板（自动 provision） |

## 启动

```bash
# IPAM 主栈已启动（网络 ipam_default 存在）
cp observability/alertmanager.yml.example observability/alertmanager.yml   # 按需改收件人
GRAFANA_ADMIN_PASSWORD='strong-password' \
docker compose -f observability/compose.observability.yaml --profile observability up -d
```

访问：Prometheus `http://127.0.0.1:9090`、Alertmanager `:9093`、Grafana `:3000`。

## 指标

`GET /metrics`（无需认证，建议只在内网/监控网暴露）：

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
| `ipam_log_parts` | 活跃 part 数 |
| `ipam_log_latest_age_seconds` | 最新日志滞后秒数 |
| `ipam_host_disk_total_bytes` / `free_bytes` | 宿主磁盘总量/可用 |

告警：`IpamLogPartsHigh`、`IpamLogIngestStalled`、`IpamLogDiskGrowthFast`、`IpamLogDiskRatioHigh`。
