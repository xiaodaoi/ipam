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
- 下载：页面“下载”或 `GET /system/backups/{id}/download`；
- 超时预算（M6-028）：`IPAM_CH_EXPORT_TIMEOUT`（单月日志导出/恢复，默认 `30m`）、`IPAM_BACKUP_TIMEOUT`（单次备份总预算，默认 `2h`）；大日志量部署按实测调大并同步放大；
- 失败处理：状态置 `failed` 并给出可读原因，半成品目录自动删除；导出文件会校验 Parquet 结束标记，避免把 ClickHouse 流式超时的半截文件记录为成功备份；
- 增量复用（M6-030）：历史月份行数未变化时硬链接上一份备份的 Parquet，仅重导有新增数据的月份；记录里的 `sizeBytes` 为逻辑大小，实际占用用 `du`/`df` 查看。

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
- 磁盘连续 3 次 ≥90% 自动切换为仅异常，连续 3 次 ≤85% 自动恢复为用户策略；85–90% 为回差区间保持现状；
- 自动降级只在渲染层生效，不修改用户保存的策略，页面「采样策略」显示实际生效模式；
- 阈值可用环境变量 `IPAM_LOG_DEGRADE_HIGH`（默认 90）/ `IPAM_LOG_DEGRADE_LOW`（默认 85）调整。

外发/入库积压观测（M6-008）：Vector 暴露自身指标（容器内 `vector:9598`，Prometheus `ipam-vector` job 抓取）：

- `vector_buffer_byte_size{component_id="clickhouse"}` / `vector_buffer_max_byte_size` 比值 ≈ 缓冲占用率（上限 1GiB，有界 `drop_newest`）；
- 告警 `IpamLogSinkBacklog`（>50% 持续 15m）、`IpamLogSinkBufferFull`（>90% 持续 5m，即将丢弃最新日志）、`IpamLogSinkErrors`（sink 发送错误）；
- 排查：确认 ClickHouse/SIEM 目标可达与吞吐；缓冲满时最新日志会被丢弃（优先保宿主稳定），处置后自动回落后再评估是否需要回填。

## 授权与终端计量（M6-004B）

- 活跃终端 = 近 24 小时 DHCP 日志中去重的客户端 MAC（60s 缓存）；`/system/license` 与 `/metrics` 共用同一份计量；
- 授权上限 `maxTerminals=0` 表示不限；**超限只告警不中断**：页面显示「终端超限」标签，message 给出超出数量；
- 指标：`ipam_license_terminals_used`、`ipam_license_terminals_limit`；告警 `IpamLicenseTerminalsOverLimit`（持续 6h）；
- 终端数依赖日志采集链路（Vector → ClickHouse），采集停滞时计量滞后但不影响业务；
- 同一设备更换网卡/随机化 MAC 会被重复计数，属 MAC 口径的已知边界。

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
| `ipam_log_export_degraded` | 采样是否因磁盘水位自动降级 | =1 时清理归档/扩容 |
| `ipam_host_disk_free_bytes` | 宿主可用空间 | 提前扩容 |

建议：

- 在线保留 180–365 天；多年合规留存用按月 Parquet 归档；
- **容量口径（M6-016 实测）**：真实日志约 44.6 字节/行（压缩后含索引）⇒ 500 万行/天 ≈ 223MB/天，180 天 ≈ 40GB，365 天 ≈ 81GB；整机磁盘建议 ≥ 在线数据 2 倍（合并/归档/备份/镜像）；
- **查询性能（M6-016 实测）**：单日 500 万行时全天 count 55ms、首页 50 行 44ms、域名子串 68ms、客户端 Top10 45ms；7 天全窗口分页在 10 并发下约 14 RPS，日志页默认时间窗应 ≤24h；
- 仪表盘快照默认缓存 5s（`IPAM_DASHBOARD_CACHE_TTL`，0=关闭）；多管理员轮询时不要关，否则每次都要聚合 ClickHouse；
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

## 会话管理（M6-007C）

入口：

- 用户自助：个人中心 → 安全设置 → 登录会话；
- 管理员：系统管理 → 账号管理 → 行末「会话」。

说明：

- 列表展示设备（User-Agent 解析）、来源 IP、登录时间、最近活动与当前会话标记；
- 「下线」会立即吊销该会话的访问令牌与刷新令牌，设备需重新登录；
- 「注销全部会话」提升账号 token_version，所有设备同时失效；
- 管理员重置口令、变更角色、停用账号、重置 2FA 时，会自动下线该账号全部会话；
- 最近活动时间每分钟最多更新一次，用于粗略判断会话是否仍在使用；
- 会话表仅保留元数据，不保存令牌原文；过期超过 7 天的记录自动清理。

排查并发登录异常时，可先让用户提供会话列表截图（设备/IP/时间），
再在管理端按账号核对；若发现未知会话，立即强制下线并建议修改口令、重置 2FA。

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

## 网络安全加固（M6-017）

### 反向代理与客户端 IP

默认**不信任任何代理**：`ClientIP()` 取自直连地址，`X-Forwarded-For` 无法伪造（防限流绕过/审计 IP 污染）。经 Nginx/HAProxy 等反代时须显式声明可信来源，否则所有请求会被视为来自反代单一 IP：

```bash
# .env
IPAM_TRUSTED_PROXIES=172.18.0.0/16,127.0.0.1/32
```

反代需正确设置 `X-Forwarded-For`/`X-Forwarded-Proto`（Nginx：`proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;`，`proxy_set_header X-Forwarded-Proto $scheme;`）。TLS 终结在反代之后时设 `IPAM_FORCE_HSTS=1` 以正常下发 HSTS。

### 请求体与 HTTP 方法

- `IPAM_MAX_BODY_BYTES`（默认 1 MiB）限制 `/api/v1/*` 请求体；超限返回 413（RFC 9457）；
- `TRACE`/`TRACK` 一律 405；
- `MaxHeaderBytes` 收敛到 64 KiB；`ReadHeaderTimeout` 10s 防慢速头；`IPAM_HTTP_READ_TIMEOUT`（默认 60s）。写超时默认不设以兼容 `/logs/tail` SSE，需要时用 `IPAM_HTTP_WRITE_TIMEOUT`（会中断长连接 SSE）。

### 安全响应头与 CSP

默认下发 `X-Content-Type-Options`、`X-Frame-Options`、`Referrer-Policy`、`Permissions-Policy`、`COOP`、`CORP`、`X-Permitted-Cross-Domain-Policies`、`X-DNS-Prefetch-Control`、`X-XSS-Protection: 0`；TLS 时含 HSTS。

内置 CSP 对 `index.html` 的内联引导脚本按 **sha256 哈希**放行（不使用 `'unsafe-inline'`），`connect-src`/`img-src` 仅 `'self'`；图标集合已**离线打包**（M6-018），运行期不访问任何外部 CDN。如需完全自定义：

```bash
IPAM_SECURITY_CSP="default-src 'self'; script-src 'self'; ..."
# 或完全关闭（不推荐）：IPAM_SECURITY_CSP_OFF=1
```

> 若前端升级后 CSP 报错，多为内联脚本哈希变化；服务启动时按实际 `index.html` 重新计算，通常无需干预。
> 新增/变更图标后须重跑 `node scripts/gen-offline-icons.mjs`（`make gen-icons`）并提交，`make gen-icons-check` 为 CI 门禁。

### TLS

- `IPAM_TLS_MIN_VERSION`（默认 `1.2`，可设 `1.3`）；TLS1.2 仅启用 AEAD 套件（ECDHE+GCM/CHACHA20），禁用重协商；
- 自签证书仅供试用，正式交付替换为企业 CA。

```bash
# 自检：协议与套件
openssl s_client -connect 127.0.0.1:8443 -tls1_1 </dev/null 2>&1 | grep -i "alert\|handshake"
openssl s_client -connect 127.0.0.1:8443 -tls1_2 </dev/null 2>&1 | grep -i "cipher is"
# 安全头
curl -skD- https://127.0.0.1:8443/ -o /dev/null | grep -iE "content-security|strict-transport|x-frame|cross-origin"
```

### /metrics 暴露面

设置 `IPAM_METRICS_TOKEN`（install.sh 自动生成）后，`/metrics` 需 `Authorization: Bearer <token>`；未设置时对所有可达客户端开放并在启动日志告警。监控抓取令牌写入 `observability/metrics_token`。

### 快速扫描自检

```bash
# 依赖与镜像漏洞（需网络；只读扫描）
docker scout cves ipam/control-plane:0.1.0-dev 2>/dev/null || true
# 镜像 HIGH/CRITICAL CVE（M6-019；无本地 trivy 时经 docker 运行 aquasec/trivy）
make scan-images            # 或 bash scripts/scan-images.sh
# Dockerfile/compose 误配（M6-019）
trivy config --severity HIGH,CRITICAL deploy    # 例外见仓库根 .trivyignore.yaml
# API 未授权访问
curl -sk -o /dev/null -w '%{http_code}\n' https://127.0.0.1:8443/api/v1/subnets   # 期望 401
# TRACE 方法
curl -sk -o /dev/null -w '%{http_code}\n' -X TRACE https://127.0.0.1:8443/        # 期望 405
# 超大请求体
head -c 2000000 /dev/zero | curl -sk -o /dev/null -w '%{http_code}\n' \
  -X POST --data-binary @- -H 'Content-Type: application/json' https://127.0.0.1:8443/api/v1/auth/login  # 期望 413
```

### 镜像与容器加固（M6-019）

- **发布门禁**：`release.yml` 的 `image-scan` job 对推送的每个镜像跑 trivy（HIGH/CRITICAL，可修复即失败），
  并把 SARIF 上传到代码扫描；失败会阻断 `release-notes`。周期扫描见 `security.yml`（`trivy fs` + `trivy config`）。
- **运行时约束**：所有服务均设 `no-new-privileges:true`；`control-plane`/`coherence-daemon`/`vector` 额外
  `cap_drop: ALL` 且以非 root 运行（control-plane uid 10001）。
- **有意以 root 运行**：`unbound`（需绑定 53 并内部降权）、`kea-dhcp4/6`（需绑定 67/547 与 `raw` socket）
  已记录于 `.trivyignore.yaml` 并附带理由；`coherence-daemon` 非 root 化待共享 `/run/ipam` 属主重构。
- **本地**：`make scan-images`（无本地 trivy 时自动经 docker 运行）；`--strict` 使 trivy/docker 均缺失时判失败。
