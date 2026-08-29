# [M2-018] DHCPv6 管理落地（PD 前缀委派）

| 字段 | 内容 |
|---|---|
| ID | M2-018 |
| 状态 | review |
| 来源 | 架构文档 §561（DHCP 菜单 3/6）：DHCPv6 子网+地址池+PD 前缀委派 |
| 负责 | opencode(backend/frontend) |
| 创建 | 2026-08-28 |
| 更新 | 2026-08-28 |

## 目标

DHCP 菜单 6/6 收口：v6 子网的池支持 PD（前缀委派）语义——Pool 数据模型扩展（prefixLen/delegatedLen）、kea BuildConfig subnet6/pd-pools 投影、前端 v6 池表单。

## 方案

- 数据：address_pool 加 prefix_len/delegated_len 列（迁移 0012）；pd 池 startAddr=委派前缀，endAddr 由后端推导（prefix+prefix-len 范围尾）
- Kea：subnet6 投影（dynamic 池 → pools；pd 池 → pd-pools，含 prefix-len/delegated-len）
- 前端：子网页 family=6 池表单 kind 选择 + len 输入

## 验收标准（可测）

- [x] spec AddressPool 加 prefixLen/delegatedLen + gen；迁移 0012
- [x] kea BuildConfig subnet6/pd-pools 投影单测锚定
- [x] 前端 v6 池表单；typecheck/零外链 PASS
- [x] e2e：v6 子网+pd pool → kea 实收 pd-pools（日志实证）
- [x] lint 0、commit [M2-018]

## 实施记录（追加式，勿删旧条目）

### 2026-08-28 · 会话1
- **做了**：spec AddressPool 加 prefixLen/delegatedLen（kind=pd 语义）+ gen；迁移 0012（runner 自动应用）；Pool 结构/Pg 读写扩展（pd 池 endAddr 由 pdRangeEnd 推导：prefix+prefix-len 范围尾）；**kea BuildConfig6**（subnet6：dynamic→pools、pd→pd-pools）；**DeploySubnet/applyDhcpFn 扩展 dhcp6 config-set**（v6 子网存在时同步下发）；前端子网页 v6 池表单（kind Select + len 输入）。
- **验证结果**：kea 单测锚定（pd-pools JSON 结构断言）+ 全链绿 + lint 0 + typecheck 0 + 零外链；容器 e2e——v6 子网+PD 创建 201（endAddr 推导回填 ✓ keaSubnetId=4/5 本地递增 ✓）→ PATCH 触发全量下发 → **kea-dhcp6 日志实证 reconfigure 实收 2 个 v6 子网（DHCPSRV_CFGMGR_NEW_SUBNET6 ×2 + DHCP6_CONFIG_COMPLETE）**。
- **踩坑留痕**：① 断言语义两修（缺 len 的池剔除但子网保留；字符串包含断言改 JSON 结构断言）；② v4 过滤断言（BuildConfig6 只出 v6）。
- **遗留**：① kea-dhcp6 的 control socket 在 reconfigure 后不稳定（Kea 2.2 行为——force-recreate 恢复；config-set 实收不受影响，控制通道查询受限）→ 升级 Kea 跟进；② PD 租约查询/委派生命周期 P2；③ 子网 GET 单查端点缺（列表够用）记录。
