# IPAM 一体化 DDI 管理平台

IPAM、DHCPv4/v6（Kea）、DNS（Unbound）与统一日志（Vector + ClickHouse）一体化的内网 DDI 产品。控制面 Go + Gin，管理界面 Vue 3 + Vben Admin v5，单仓库交付。

> 架构与需求基线：`docs/架构设计文档-V1.md`、`IPAM需求调研报告.md`。
> 实施计划与任务看板：`docs/pm/plans/M6-商业化就绪实施计划.md`、`docs/pm/board/`。

## 能力概览

| 模块 | 能力 |
|---|---|
| IPAM | 组织树、子网/地址池、地址六态台账、保留/静态绑定、资产登记、CSV/批量操作 |
| DHCPv4/v6 | 作用域/池、排除段、PD 前缀委派、选项与类匹配、租约、预留、Option79 双栈联动 |
| DNS | 上游与条件转发、本地正/反区域、缓存、RRL、DNSSEC、RPZ 封禁、订阅源、解析测试台 |
| 日志中心 | 检索/TopN/QPS、实时流、操作审计、留存/归档、Syslog/Kafka/Webhook 外发 |
| 平台 | JWT/刷新令牌、RBAC（6 域×读写）、组织主数据、站点品牌、TLS、备份恢复、License、TOTP 2FA |
| 可运维 | `/healthz`、`/readyz`、`/metrics`、Prometheus/Grafana/告警、备份恢复、审计链校验 |

## 快速开始（单机 Docker）

前置：Ubuntu 22.04+、Docker 24+、Docker Compose v2、2C4G/40GB 起。

```bash
git clone <repo> && cd ipam/deploy/compose
bash install.sh
```

脚本会：

1. 生成 `.env`（随机 PostgreSQL 口令、JWT 密钥）；
2. 生成自签 TLS 证书 `certs/server.crt|key`；
3. `docker compose up -d --wait`；
4. 通过 `https://127.0.0.1:8443/healthz` 冒烟；
5. 打印一次性初始 admin 口令（或读取 `/run/ipam/initial-admin-password`）。

访问 `https://<host>:8443`。默认不强制首次改密；需要时在「系统管理 → 账号安全」开启。正式环境请替换 `certs/` 为企业 CA 证书。

## 端口

| 端口 | 用途 |
|---|---|
| 8443/tcp | 控制面 UI/API（HTTPS） |
| 53/udp,tcp | Unbound DNS |
| 67/udp、547/udp | Kea DHCPv4/v6 |
| 8000/tcp | Kea Ctrl Agent（仅本机/内部） |
| 5432/tcp | PostgreSQL（仅 127.0.0.1） |

## 文档

- 安装/升级/备份/回滚：`docs/manual/`
- 使用指南：`docs/manual/user-guide.md`
- API：`https://<host>:8443/openapi.json`（OpenAPI 3.1，唯一事实源）
- 实现说明：`docs/dev/`
- 任务卡/进度：`docs/pm/`

## 安全默认值

- 控制面默认 HTTPS（自签可用，生产替换 CA）；HSTS/安全响应头；
- JWT 密钥由部署生成，禁用生产随机密钥；
- 空库随机 admin 口令；口令策略 ≥10 位+3 类字符；
- 可选 TOTP 2FA、强制首次改密；
- 审计记录脱敏请求体 + SHA256 链哈希，可校验完整性；
- 备份包含 PostgreSQL/配置/ClickHouse 日志 Parquet。

## License

离线授权文件（Ed25519）。未激活为 community；激活后展示版本/能力/到期时间。签发工具：

```bash
go run ./cmd/licensegen -gen-key
IPAM_LICENSE_PRIVATE_KEY=<base64> go run ./cmd/licensegen \
  -id LIC-001 -to "ACME" -edition enterprise -features ha,ldap,siem -max 5000 -days 365 -out license.json
```

客户侧配置 `IPAM_LICENSE_PUBKEY` 与 `IPAM_LICENSE_FILE`，在「系统管理 → 授权管理」激活。

## 高可用边界

当前单机交付只提供**单机韧性 + 冷备恢复**：

- Docker healthcheck/restart、systemd 自启、看门狗；
- 备份/恢复演练、监控告警；
- **不承诺宿主整机故障自动接管**。

真正双机 HA（keepalived VIP + Kea hot-standby + config-replicate）需要 2 台独立主机/VM（不同电源/网络故障域），见 `docs/pm/board/backlog/M6-003-高可用主备.md`。

## 开发

```bash
make doctor   # 工具链自检
make dev      # 宿主跑控制面 + 容器依赖
make dev-web  # 前端 Vite HMR
make test     # go test + web 单测
make lint     # golangci-lint + eslint/oxlint
make gen      # 从 api/openapi 再生 api/gen（禁止手改）
make build    # 构建二进制 + 前端
```

## 许可

产品代码许可见 `LICENSES.md`；第三方组件（Kea MPL-2.0、Unbound BSD-3、Vben MIT 等）合规矩阵同步维护。禁止引入 GPL 依赖。
