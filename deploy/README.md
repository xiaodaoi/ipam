# deploy/ — 交付物

- `images/`：各服务 Dockerfile（Ubuntu jammy 基座；**unbound 必须走 NLnetLabs 官方源固定 ≥1.16**，K8 风险约束）。postgresql/clickhouse 使用官方镜像不在此定制
- `compose/`：默认交付形态——compose.yaml + install.sh（含预检脚本）
- `helm/`：K8s Chart，数据面 DaemonSet(hostNetwork) + PV，与 compose 共用镜像 tag（D9）

端口矩阵与容器能力边界见架构文档 §7。
