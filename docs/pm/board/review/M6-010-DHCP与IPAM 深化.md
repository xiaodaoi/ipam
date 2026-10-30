# [M6-010] DHCP/IPAM 深化：010A 租约管理；010B 台账与探测

| 字段 | 内容 |
|---|---|
| ID | M6-010 |
| 状态 | 010A review；010B backlog |
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

## 010B 未完成（backlog）

- RA 联动（M/O/PIO）配置与状态；
- option82 展示与基于 circuit-id 的选池策略；
- Ping/ARP 冲突探测与自动隔离；
- 子网/池利用率历史采样与阈值告警（当前仅仪表盘实时 TopN）；
- 新终端发现/影子资产；
- CSV 导入导出、VLAN/站点/负责人、逐 IP 备注标签。

## 验收

- [x] v4/v6 租约可查询、释放；v4 可延期/改绑；
- [x] 页面可用且刷新后状态一致；
- [x] 单测/lint/typecheck/build/gen-check 通过；
- [x] 真实 Kea 环境端到端验证；
- [ ] 010B 项（RA/option82/冲突/利用率历史/发现/CSV/VLAN）。

## 涉及文件

- `api/openapi/{paths/dhcpoption.yaml,components/schemas/dhcpoption.yaml}`、`api/gen/**`
- `internal/engine/lease/lease.go`（共享租约模型，避免 kea↔dhcp 循环依赖）
- `internal/engine/kea/lease.go`、`lease_test.go`
- `internal/module/dhcp/lease_handler.go`、`lease_handler_test.go`
- `cmd/control-plane/main.go`、`internal/module/platform/handler_test.go`
- `web/apps/web-ipam/src/views/dhcp/leases/index.vue`、`api/ipam.ts`、路由/文案
- 文档：本卡、实现说明、用户指南、进度日志

## 实施记录

### 2026-10-30

- 新增 Kea 租约客户端：lease4-get-all/get/update/del、lease6-del；租约模型独立为 `internal/engine/lease`。
- 新增 `LeaseHandler` 与 4 个端点；校验 MAC/时长/类型，更新前检查存在性（404）。
- 新增「租约管理」页，v4/v6 分页签、搜索、延期/改绑/释放。
- 实测：合成租约 10.61.40.251 延期 600→1800、改绑 MAC、释放后 Kea result=3；IPv6 合成租约释放成功。
- 验证：go test（dhcp/kea/platform）、go build、gen-check、vue-tsc、eslint、前端 build 通过。
