# [M2-019] 子网级 DHCP 选项（网关/DNS，v4+v6）

| 字段 | 内容 |
|---|---|
| ID | M2-019 |
| 状态 | review |
| 来源 | 用户需求：DHCP 应下发网关/DNS/掩码等且按子网后台可配（v4+v6）；§13.4 DHCPv4/v6 管理 |
| 负责 | opencode(backend/frontend) |
| 创建 | 2026-08-28 |
| 更新 | 2026-08-28 |

## 背景（缺口）

Kea 投影现状只有地址池（pools/pd-pools）+ 全局 option-data（M2-016）——**子网级 option-data（网关/DNS）缺失**：客户端拿到地址但无网关/DNS，DHCP 实际不可用。掩码由 CIDR 隐含 ✓。

## 方案

- 数据：subnet 加 gateway text（v4 routers）、dns_servers text（逗号分隔，v4 domain-name-servers / v6 dns-servers）——迁移 0014
- Kea 投影：BuildConfig 的 subnet 元素加子网级 option-data（非空时；v4 routers/domain-name-servers、v6 dns-servers）
- spec：SubnetCreate/Subnet/SubnetUpdate 加 gateway/dnsServers
- 前端：子网页表单（v4 显示网关+DNS；v6 显示 DNS）+ 列表展示

## 验收标准（可测）

- [x] spec + gen；迁移 0014（runner 自动应用）
- [x] kea BuildConfig v4 subnet option-data（routers/domain-name-servers）+ v6（dns-servers）投影单测锚定
- [x] 前端子网表单 + 列表；typecheck/零外链 PASS
- [x] e2e：v4+v6 子网带网关/DNS 创建 → kea config-get 实收子网级 option-data
- [x] lint 0、commit [M2-019]

## 实施记录（追加式，勿删旧条目）

### 2026-08-28 · 会话1
- **做了**：spec Subnet/SubnetCreate/SubnetUpdate 加 gateway/dnsServers + gen；迁移 0014（runner 自动应用）；Subnet 结构/Pg 读写扩展（subnetCols/scanSubnet/Create/Update）；kea BuildConfig v4 subnet option-data（routers/domain-name-servers）+ BuildConfig6 v6 dns-servers；单测锚定 ×2；前端子网表单（v4 网关+DNS/v6 DNS）+ 列表网关列。
- **验证结果**：全链测试绿（含 kea 子网级 option-data 锚点 ×2）+ lint 0 + typecheck 0 + 零外链；容器 e2e 决定性通过——v4 子网（gateway 10.99.3.1/dns 223.5.5.5+114）创建 201 → **kea dhcp4 config-get 实收 subnet 6 的 option-data（routers=10.99.3.1、domain-name-servers=223.5.5.5, 114.114.114.114）**；v6 子网（dns 2406:174::53）→ kea6 恢复后 PATCH 重触发 → **kea-dhcp6 日志实证 reconfigure 实收 3 个 v6 子网（含 2406:174 的 dns-servers）**。
- **踩坑留痕**：① INSERT 列清单与 VALUES/参数三处同步缺一（列清单漏 gateway/dns_servers → INSERT has more expressions）；② applyDhcpFn 的 v6 失败连坐 v4（503）→ v6 降级软失败（log 记录，kea6 恢复后 PATCH 重触发全量补齐——实测 kea6 恢复后 NEW_SUBNET6 实证）；③ python heredoc 三引号嵌套多次静默失败 → 改 write/Edit 工具精确锚定。
- **遗留**：kea-dhcp6 control socket reconfigure 后不稳定（Kea 2.2 行为）→ 升级跟进（M2-018 遗留延续）；子网级 option-data 的 UI 编辑（PATCH 表单）P2。
