# [M6-012B] Helm Chart 交付（需 K8s 集群）

| 字段 | 内容 |
|---|---|
| ID | M6-012B |
| 状态 | blocked（需 K8s 集群验证） |
| 里程碑 | M6 商业化就绪 |
| 优先级 | P2（影响 Helm 交付形态，不阻塞五类文档） |
| 依赖 | M6-012A 文档与离线包 |
| 负责 | devops |
| 创建 | 2026-11-16 |
| 更新 | 2026-11-16 |

## 目标

为已有 K8s 客户提供可验证的 Helm Chart：数据面 DaemonSet（hostNetwork、nodeSelector 打标边缘节点）+ 控制面 Deployment + PV/SC + keepalived VIP。

## 前置条件

- K8s 1.28+，至少 2 个可调度节点；
- CNI/PSP 策略允许 hostNetwork、NET_ADMIN、privileged（keepalived 唯一特权容器）；
- 可用的 StorageClass（PG/CH/Vector 持久卷）；
- 集群内可访问镜像仓库（或离线 `docker load` + `imagePullPolicy: Never`）。

## 实施范围

1. **Chart 骨架**：`deploy/helm/ipam/`（Chart.yaml、values.yaml、templates/）；
2. **数据面 DaemonSet**：unbound / kea-dhcp4 / kea-dhcp6 / kea-ctrl-agent / vector，hostNetwork + nodeSelector；
3. **控制面**：Deployment + Service + Ingress（可选 TLS）；
4. **持久化**：PostgreSQL / ClickHouse StatefulSet 或 external 模式；PVC 模板与 SC 参数；
5. **keepalived**：DaemonSet 或静态 Pod，VIP 与 VRRP 配置；
6. **values 分层**：`values-dev.yaml`（单节点）/ `values-ha.yaml`（双节点）；
7. **文档**：`docs/manual/install-k8s.md`（前置、安装、升级、卸载、排障）。

## 验收标准

- [ ] `helm lint` / `helm template` 通过；
- [ ] 在真实 K8s 集群 `helm install` 成功，全部 Pod Ready；
- [ ] hostNetwork 数据面保留客户端真实源 IP（DNS/DHCP 日志验证）；
- [ ] PG/CH 数据落在 PVC，Pod 重建后数据不丢；
- [ ] 双节点 values 下 VIP 漂移 ≤30s（与 M6-003B 共用演练结论）；
- [ ] `helm upgrade` / `helm rollback` 可回滚；
- [ ] 安装文档经他人按文档实操通过。

## 涉及模块（预计）

- `deploy/helm/ipam/**`
- `deploy/compose/compose.yaml`（抽取共用配置）
- `docs/manual/install-k8s.md`
- `.github/workflows/release.yml`（可选：chart 打包发布）

## 阻塞说明

当前环境无 K8s 集群；按纪律不提供未经验证的 Chart，避免「看起来能部署但实际失败」。集群到位后解除 blocked。

## 实施记录（追加式，勿删旧条目）

### 2026-11-16 · 看板收口

- 由 `M6-012-文档与交付.md` 的 012B 拆出独立卡；012A（README/五类文档/离线包）已归档 `done/`。
