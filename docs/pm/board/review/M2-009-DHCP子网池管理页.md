# [M2-009] DHCP 子网与地址池管理页

| 字段 | 内容 |
|---|---|
| ID | M2-009 |
| 状态 | review |
| 来源 | §13.4 DHCPv4/v6 管理；M2-002 已交付 subnets API |
| 负责 | opencode(frontend) |
| 创建 | 2026-08-28 |
| 更新 | 2026-08-28 |

## 目标
子网+地址池 CRUD 页：组织节点下拉（主数据引用）、family 选择、CIDR、池区间管理、Kea 下发状态（keaSubnetId 回显）。

## 验收标准
- [ ] 列表展示子网与池（orgId 过滤）
- [ ] 新建表单（org/family/cidr/池起止）→ keaSubnetId 回显
- [ ] 删除（引用保护错误展示）
- [ ] typecheck/build/零外链 PASS

## 涉及模块
- web/apps/web-ipam/src/{api,views/dhcp/subnets,router,locales}

## DoD 自检
- [ ] typecheck 通过
- [ ] lint 通过
- [ ] 进度日志
- [ ] API 已存在（M2-002）
- [ ] commit 带 [M2-009]

## 实施记录（追加式，勿删旧条目）

### 2026-08-28 · 会话1
- **做了**：子网管理页（组织树筛选+新建表单 org/family/cidr/池起止+keaSubnetId 回显 Tag+删除）；api/ipam.ts 扩 listOrgs/listSubnets/createSubnet/deleteSubnet。
- **踩坑**：① 消重脚本 bug 把 ipam.ts 清成 0 字节——git checkout 秒恢复（教训：脚本化编辑前后必须 wc 校验）；② listSubnets/listOrgTree 与旧版重复定义（TS2451）去重。
- **验证结果**：typecheck/build/零外链 PASS；容器部署 /dhcp/subnets 200。
- **遗留**：v6 PD 池类型 P1；池行内编辑 P1。
