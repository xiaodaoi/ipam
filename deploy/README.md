# deploy/ — 交付物

- `images/`：各服务 Dockerfile（Ubuntu jammy 基座；**unbound 必须走 NLnetLabs 官方源固定 ≥1.16**，K8 风险约束）。postgresql/clickhouse 使用官方镜像不在此定制
- `compose/`：默认交付形态——compose.yaml + install.sh（含预检脚本）
- `helm/`：K8s Chart，数据面 DaemonSet(hostNetwork) + PV，与 compose 共用镜像 tag（D9）

端口矩阵与容器能力边界见架构文档 §7。

## 安全默认值（M6-001）

- **HTTPS**：`install.sh` 首次运行会生成 `certs/server.crt|key`（自签，CN=主机名，SAN 含 localhost/127.0.0.1），并把容器内路径写入 `.env` 的 `IPAM_TLS_CERT/IPAM_TLS_KEY`。正式环境请替换为企业 CA 证书；私钥权限建议 640 且仅容器运行用户可读。
- **JWT 密钥**：`install.sh` 自动生成 `IPAM_JWT_SECRET`（64 位 hex）。手工部署必须在 `.env` 设置 ≥32 字节随机值；缺失时控制面会拒绝启动（compose `:?` 校验）。
- **初始口令**：空库首次启动随机生成 admin 口令，写入容器 `/run/ipam/initial-admin-password`（0600）并打印在 `docker compose logs control-plane`。**默认不强制首次改密（便于调试）**；如需强制，在「系统管理 → 系统设置 → 账号安全」开启，开启后对新建账号/重置口令生效。`IPAM_POC_PASSWORD` 仅用于自动化/兼容。
- **限流**：`IPAM_RATE_LIMIT`（次/分钟/IP，默认 600，0=关闭）；登录固定 5 次/分钟。
- **安全头**：`X-Content-Type-Options`、`X-Frame-Options`、`Referrer-Policy`、`Permissions-Policy`；HTTPS 时附 HSTS。需要 CSP 时通过 `IPAM_SECURITY_CSP` 显式配置。
- **容器加固**：非数据面服务启用 `no-new-privileges` + `cap_drop: ALL`；Kea/Unbound 因绑定低端口保留必要能力。
- 自签证书仅用于内网试用；对外交付前必须替换并做证书到期监控（M6-005）。
