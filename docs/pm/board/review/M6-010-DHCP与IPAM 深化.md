# [M6-010] DHCP/IPAM 深化：010A 租约管理；010B 台账与探测

| 字段 | 内容 |
|---|---|
| ID | M6-010 |
| 状态 | 010A + 010B1 + 010B2 + 010B3 + option82 展示 review；RA/选池/冲突 backlog |
| 里程碑 | M6 商业化就绪 |
| 优先级 | P1 |
| 依赖 | M6-001 |
| 创建 | 2026-10-29 |
| 更新 | 2026-10-30 |

## 010A 已完成：租约管理

- **API**（Kea lease_cmds 实时操作，不入库）：
  - `GET /dhcp/leases4`：DHCPv4 租约列表（IP/MAC/client-id/主机名/子网/state/剩余时长）；
  - `PATCH /dhcp/leases4/{ipAddress}`：延期（validLifetime）、改绑（hwAddress）、改主机名；
  - `DELETE /dhcp/leases4/{ipAddress}`：释放（幂等，不存在也成功）；
  - `DELETE /dhcp/leases6/{ipAddress}?leaseType=IA_NA|IA_PD`：释放 IPv6 租约。
- **页面**：DHCP 服务 → 租约管理。
  - v4/v6 分页签、KPI（v4 总数/冲突、v6 总数/PD）、搜索（IP/MAC/主机名/DUID）；
  - v4 支持延期/改绑/释放，v6 支持释放；
  - 剩余时长按 cltt+valid-lft 计算，过期/正常/冲突着色。
- **校验**：MAC 格式、租约时长 60–31536000 秒、hostname 长度、leaseType 枚举。
- **测试**：Kea 客户端单测（list/get/update/delete 参数）、handler 单测（排序/校验/404/204）；真实 Kea 环境端到端验证延期→改绑→释放→v6 释放。

## 010B1 已完成：利用率历史与阈值告警

- **采样**：每 5 分钟（`IPAM_UTIL_SAMPLE_INTERVAL`）统计每个 IPv4 子网池的容量与已用地址（coherence 绑定 + 预留，池内去重），写入 `subnet_utilization`，保留 90 天。
- **API**：
  - `GET /dhcp/utilization` 最新快照；
  - `GET /dhcp/utilization/{subnetId}?hours=1..720` 历史序列。
- **指标/告警**：`ipam_subnet_utilization_percent{subnet,cidr}`；>85% warning（15m）、>95% critical（5m）。
- **页面**：DHCP 服务 → 利用率分析；最新快照表 + 状态着色 + 24h/3d/7d/30d 趋势图。
- **测试**：Compute 去重/池外排除/容量；MemRepo 最新/序列/保留清理；真实 PG 采样与指标实测。

## 010B2 已完成：未登记终端发现

- **API**：`GET /assets/discovered`，从 coherence 绑定中筛出 asset 表未登记的 MAC（active/grace/conflict），按最近活动倒序。
- **页面**：DHCP 服务 → 未登记终端；KPI、搜索、状态着色；「登记」弹窗预填 MAC，填写使用人/部门/组织/备注后写入资产表。
- **闭环**：登记成功后该终端从发现列表移除，可在资产页继续维护标签/组织。
- **测试**：`Discover` 单测（去重、inactive 过滤、IPv6/主机名）；真实环境发现→登记→列表移除→删除验证。

## 010B3 已完成：逐 IP 元数据与 CSV 导出

- **数据**：迁移 0034 `ledger_meta`（address 自然键、subnet_id 级联、site/vlan/purpose/user/phone/remark/custom_attrs）。
- **API**：`PUT /ledger/meta`；台账 `GET /ledger` 行内回填元数据（user 优先于资产 owner）。
- **页面**：IPv4 台账地址矩阵编辑弹窗新增站点/VLAN；保存后写库并刷新；页头「导出 CSV」导出当前网段全量地址。
- **测试**：元数据回填/owner 覆盖、内存仓储过滤/删除；真实 PG 保存→查询→清理；E2E 11/11。

## 010B4 部分完成：option82 展示

- Kea 基础配置启用 `store-extended-info`（全局 + 子网），租约记录 circuit-id/remote-id；
- 租约 API/页面新增 `circuitId/remoteId` 与「option82（circuit / remote）」列；
- 仅真实中继租约/续约后写入，直连客户端为空；`lease4-add` 合成租约不会产生该字段（已实测）。
- 基于 circuit-id 的选池（pool client-class）仍未实现，保留在 backlog。

## 010B 其余未完成（backlog）

- RA 联动（M/O/PIO）配置与状态；
- 基于 circuit-id 的选池策略（pool client-class）；
- Ping/ARP 冲突探测与自动隔离。

## 验收

- [x] v4/v6 租约可查询、释放；v4 可延期/改绑；
- [x] 页面可用且刷新后状态一致；
- [x] 单测/lint/typecheck/build/gen-check 通过；
- [x] 真实 Kea 环境端到端验证；
- [x] 利用率历史采样、趋势页、85/95% 告警指标；
- [x] 未登记终端发现与一键登记；
- [x] 逐 IP VLAN/站点/使用人/用途/备注与 CSV 导出；
- [x] option82 circuit-id/remote-id 展示（真实中继租约）；
- [ ] 010B 其余项（RA/选池/冲突探测）。

## 涉及文件

- `api/openapi/{paths/dhcpoption.yaml,components/schemas/dhcpoption.yaml}`、`api/gen/**`
- `internal/engine/lease/lease.go`（共享租约模型，避免 kea↔dhcp 循环依赖）
- `internal/engine/kea/lease.go`、`lease_test.go`
- `internal/module/dhcp/lease_handler.go`、`lease_handler_test.go`
- `cmd/control-plane/main.go`、`internal/module/platform/handler_test.go`
- `web/apps/web-ipam/src/views/dhcp/leases/index.vue`、`api/ipam.ts`、路由/文案
- `internal/module/utilization/**`、`db/postgresql/migrations/0033_subnet_utilization.sql`
- `observability/alerts.yml`、`web/.../views/dhcp/utilization/index.vue`
- `internal/module/ipam/{asset,asset_handler}.go`、`web/.../views/dhcp/ledger/discovered.vue`
- `internal/module/ipam/{ledger_meta,ledger_meta_handler}.go`、`db/.../0034_ledger_meta.sql`
- `web/.../components/ip-plan-map.vue`、`web/.../views/dhcp/ledger/v4.vue`
- `internal/engine/kea/config.go`、`internal/engine/lease/lease.go`、`web/.../views/dhcp/leases/index.vue`
- 文档：本卡、实现说明、用户指南、进度日志

## 实施记录

### 2026-10-30

- 新增 Kea 租约客户端：lease4-get-all/get/update/del、lease6-del；租约模型独立为 `internal/engine/lease`。
- 新增 `LeaseHandler` 与 4 个端点；校验 MAC/时长/类型，更新前检查存在性（404）。
- 新增「租约管理」页，v4/v6 分页签、搜索、延期/改绑/释放。
- 实测：合成租约 10.61.40.251 延期 600→1800、改绑 MAC、释放后 Kea result=3；IPv6 合成租约释放成功。
- 验证：go test（dhcp/kea/platform）、go build、gen-check、vue-tsc、eslint、前端 build 通过。

### 2026-10-30（010B1）

- 新增采样服务、PG/内存仓储、迁移 0033；启动即采一次、每 5 分钟一次、保留 90 天。
- 指标接入 `/metrics`，新增 85%/95% 告警规则。
- 新增「利用率分析」页与 E2E 步骤；真实环境验证 latest/series/metrics。
- 验证：utilization/platform 单测、go build、gen-check、vue-tsc、eslint、E2E 10/10。

### 2026-10-30（010B2）

- 新增 `AssetService.Discover` 与 `GET /assets/discovered`；main 注入 coherence 绑定源。
- 新增「未登记终端」页：搜索、状态、登记弹窗（使用人必填、组织可选）。
- 实测：真实租约 `00:0c:29:29:11:4f` 出现在发现列表；登记后列表为 0；删除资产后恢复。
- 验证：ipam 单测、go build、gen-check、vue-tsc、eslint、E2E 11/11。

### 2026-10-30（010B3）

- 新增 ledger_meta 表/仓储/端点；台账行回填元数据，user 覆盖 owner。
- 地址地图编辑支持站点/VLAN，保存写库；新增当前网段 CSV 导出。
- 实测 PUT/GET 元数据；修复 inet `::text` 带 /32 的键不一致（改 `host(address)`）。
- 验证：ipam 单测、go build、gen-check、vue-tsc、eslint、前端 build、E2E 11/11。

### 2026-10-30（010B4 部分）

- Kea 启用 store-extended-info；租约模型/API/页面增加 circuit-id/remote-id 展示。
- 实测 Kea 接受配置（startup converge ok）；合成租约不写 option82，需真实中继环境验证。
- 验证：kea/dhcp 单测、go build、gen-check、vue-tsc、eslint、前端 build、E2E 11/11。
