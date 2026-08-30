# [M2-033] Option 79 消费链路（DHCPv6 租约 hwaddr 投影到 API 与前端）

| 字段 | 内容 |
|---|---|
| ID | M2-033 |
| 状态 | review |
| 来源 | 地址族关联方案落定：Option 79（RFC 6939）方案必须实现。链路三段中 relay 插入属网络设备配置、Kea 原生接收无需实现——**本项目缺口是消费侧**：Lease6 投影缺 hw-address/hwtype/hwaddr-source 三字段 |
| 负责 | opencode(backend+frontend) |
| 创建 | 2026-08-29 |
| 更新 | 2026-08-29 |

## 链路责任划分

1. **Relay 插入**：核心交换机 DHCPv6 relay 开 RFC 6939（**网络设备交付要求**，非本项目代码）
2. **Kea 接收**：原生支持（option 79 / DUID-LL 解析为 hwaddr，无开关）
3. **本项目消费（本卡）**：lease6-get-all 投影补 hw 三字段 → spec/API → PD 租约卡 MAC 列 → 供 dualstack MAC 关联使用

## 验收标准（可测）

- [x] typecheck/全链绿 + 零外链 PASS
- [x] e2e 决定性：lease6-add 带 hw-address（模拟 relay 79 产物）→ GET /dhcp/leases6 回读 hwAddress=aa:bb:cc:dd:ee:01（Kea 原始响应同值，全链一致）
- [x] 文档三件套 + commit [M2-033]

## 实施记录（追加式，勿删旧条目）

### 2026-08-29 · 会话1
- **做了**：spec DhcpLease6 加 hwAddress/hwAddrSource（可选）+ gen；ctrl.go Lease6 加 hw-address/hwtype/hwaddr-source 投影；main.go lease6Fn 条件投影（非空才透传）；前端 PD 租约卡加「MAC」列。
- **验证结果**：全链绿（build/test/lint/typecheck/零外链）；e2e——lease6-del 清旧 → lease6-add 带 hw-address=aa:bb:cc:dd:ee:01 → agent 原始响应含 hw-address → API 回读 MAC 一致。
- **踩坑留痕**：lease6-add 传 hwtype/hwaddr-source **不被 Kea 持久化**（add 返回 result 0 但 get-all 无此二字段——source 是 Kea 接收真实 relay 报文时的内部推导）——**模拟验证只覆盖 hw-address；source 投影代码同构，真实 relay 部署时验收**。
- **交付要求（网络设备侧）**：核心 DHCPv6 relay 启用 RFC 6939（option 79 插入）——配置项随厂商：如华为 `dhcpv6 relay option79 enable` 类指令。
- **遗留**：dualstack 关联链的 MAC join 整合（需配合 NDP/AC 采集器，后续卡）；hwaddr-source 真实 79 报文场景验收（随 relay 交付）。
