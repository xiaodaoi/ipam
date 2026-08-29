# [M2-018] DHCPv6 管理落地（PD 前缀委派）

| 字段 | 内容 |
|---|---|
| ID | M2-018 |
| 状态 | doing |
| 来源 | 架构文档 §561（DHCP 菜单 3/6）：DHCPv6 子网+地址池+PD 前缀委派 |
| 负责 | opencode(backend/frontend) |
| 创建 | 2026-08-28 |
| 更新 | 2026-08-28 |

## 目标

DHCP 菜单 6/6 收口：v6 子网的池支持 PD（前缀委派）语义——Pool 数据模型扩展（prefixLen/delegatedLen）、kea BuildConfig subnet6/pd-pools 投影、前端 v6 池表单。

## 方案

- 数据：address_pool 加 prefix_len/delegated_len 列（迁移 0012）；pd 池 startAddr=委派前缀，endAddr 由后端推导（prefix+prefix-len 范围尾）
- Kea：subnet6 投影（dynamic 池 → pools；pd 池 → pd-pools{prefix, prefix-len, delegated-len}）
- 前端：子网页 family=6 池表单 kind 选择 + len 输入

## 验收标准（可测）

- [x] spec AddressPool 加 prefixLen/delegatedLen + gen；迁移 0012
- [x] kea BuildConfig subnet6/pd-pools 投影单测锚定
- [x] 前端 v6 池表单；typecheck/零外链 PASS
- [x] e2e：v6 子网+pd pool → kea config-get 实收 pd-pools
- [x] lint 0、commit [M2-018]

## 实施记录（追加式，勿删旧条目）
