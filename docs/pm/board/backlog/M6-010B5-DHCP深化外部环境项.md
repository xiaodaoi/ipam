# [M6-010B5] DHCP 深化：RA / circuit-id 选池 / 冲突探测

| 字段 | 内容 |
|---|---|
| ID | M6-010B5 |
| 状态 | blocked（需真实双栈/中继/RAW 探测环境） |
| 里程碑 | M6 商业化就绪 |
| 优先级 | P2 |
| 依赖 | M6-010B4（option82 展示）、M6-010A/B1–B3 |
| 负责 | backend + devops |
| 创建 | 2026-11-16 |
| 更新 | 2026-11-16 |

## 目标

关闭 M6-010B 剩余的三项 DHCP/IPAM 深化能力：

1. **IPv6 RA 联动**：Router Advertisement（M/O/PIO）配置与运行状态可视化；
2. **基于 circuit-id 的选池**：Kea `pool` + `client-class` 按中继 `circuit-id` 选池；
3. **地址冲突探测**：Ping/ARP 探测与自动隔离，冲突地址不参与下发。

## 为什么 blocked

- RA 需要真实 IPv6 路由器通告环境与双栈上游，当前单机无法验证 M/O/PIO 实际效果；
- circuit-id 选池必须经真实 DHCP 中继（option82），直连客户端不产生该字段（M6-010B4 已实测）；
- 冲突探测需要 RAW socket / ICMP 权限与可控的冲突主机，不能只靠合成租约。

## 实施范围

### 1. RA 联动

- Kea DHCPv6 配置渲染增加 `router-advertisement` 段（M/O/PIO、前缀、DNS）；
- API：`GET/PUT /dhcp/ra-settings`（或复用子网级选项），返回生效状态；
- 页面：DHCP 服务 → IPv6 设置 → RA 卡片（开关、前缀、有效期、最后下发状态）；
- 验证：真实双栈客户端能自动获取 IPv6 地址、路由与 DNS。

### 2. circuit-id 选池

- 子网池增加 `clientClass` / `circuitIdPattern` 元数据；
- Kea 渲染 `client-classes` + `pool.client-class`；
- 页面：地址池编辑增加「中继 circuit-id 匹配」字段；
- 验证：同一子网下不同 circuit-id 的请求落入不同池。

### 3. 冲突探测

- 下发前 Ping/ARP 探测（可配置并发/超时），冲突地址标记 `conflict` 并写入保留；
- 指标 `ipam_dhcp_conflict_total` + 告警；
- 页面：冲突列表 + 一键解除隔离；
- 验证：构造冲突主机，确认地址不被下发且告警触发。

## 验收标准

- [ ] 真实双栈环境：客户端经 RA 获取 IPv6 地址/默认路由/DNS；
- [ ] 真实中继环境：circuit-id 不同的请求进入对应地址池；
- [ ] 冲突探测可开关、有超时与并发上限，不阻塞主下发路径；
- [ ] 冲突地址自动隔离并可从页面解除；
- [ ] 单测覆盖配置渲染与匹配规则；真实环境验证记录归档；
- [ ] 文档：用户指南 DHCPv6/中继章节、运维手册故障处理。

## 涉及模块（预计）

- `internal/engine/kea/{config,ra}.go`
- `internal/module/dhcp/{pool,conflict}_handler.go`
- `api/openapi/paths/dhcp*.yaml`、`api/gen/**`
- `web/apps/web-ipam/src/views/dhcp/**`
- `config/kea/kea-dhcp6.conf`
- `observability/alerts.yml`

## 阻塞说明

当前环境无真实 IPv6 RA、无 DHCP 中继、无冲突测试主机；不提供未验证的配置开关，避免误以为能力已交付。环境具备后解除 blocked。

## 实施记录（追加式，勿删旧条目）

### 2026-11-16 · 看板收口

- 由 `M6-010-DHCP与IPAM 深化.md` 的「010B 其余未完成」拆出独立卡；010A/010B1–010B4 已归档 `done/`。
