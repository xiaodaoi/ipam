# [M4-008] 子网 CIDR 点击跳转台账错页修复

| 字段 | 内容 |
|---|---|
| ID | M4-008 |
| 状态 | done |
| 来源 | 用户现场反馈；M2-009 子网池管理页 / M2-006 台账 |
| 负责 | opencode(frontend) |
| 创建 | 2026-09-20 |
| 更新 | 2026-09-20 |

## 目标

子网管理页点击 CIDR 应进入**对应地址族**的台账，并预选该网段。

## 验收标准（可测）

- [x] IPv6 表格点 CIDR → `/dhcp/ledger/v6?cidr=<cidr>`，且该网段被预选高亮
- [x] IPv4 表格点 CIDR → `/dhcp/ledger/v4?cidr=<cidr>`，且该网段被预选
- [x] 直接访问 `/dhcp/ledger?cidr=...` 时 query 不再被 redirect 丢弃
- [x] vue-tsc 0 error；浏览器控制台无错误

## 涉及模块

- `web/apps/web-ipam/src/views/dhcp/subnets/index.vue`
- `web/apps/web-ipam/src/views/dhcp/ledger/v6.vue`
- `web/apps/web-ipam/src/router/routes/modules/dhcp.ts`

## DoD 自检（完成前逐条勾选）

- [x] 核心逻辑有单测 —— 前端不适用；以真实浏览器端到端验证替代
- [x] lint/typecheck 通过（vue-tsc 0 error）
- [x] 相关文档章节已同步（进度日志）
- [x] API 变更已先行修改 spec —— 本次无 API 变更
- [x] commit 带 `[M4-008]`

## 实施记录（追加式，勿删旧条目）

### 2026-09-20 · 会话1

- **根因1**：v4/v6 两个表格的 CIDR 链接都硬编码 `/dhcp/ledger?cidr=...`，而该路由 `redirect: '/dhcp/ledger/v4'` → IPv6 一律落到 v4 台账。
- **根因2**：静态字符串 redirect 会**丢弃 query**，故 `?cidr=` 实际未生效（v4 也未能预选，仅因落对页面未被发现）。
- **根因3**：`v6.vue` 未读取 `route.query.cidr`（`v4.vue` 有读）。
- **修复**：两个表格分别显式跳 `/dhcp/ledger/v4|v6?cidr=...`；父路由 redirect 改为函数形式并保留 `to.query`；`v6.vue` 在 `loadSubnets()` 前按 `route.query.cidr` 预置 `selectedCidr`（后续回退逻辑会保留合法值）。
- **验证**：点 `2406:440:3c16:4006:10:193:135:0/112` → URL `/#/dhcp/ledger/v6?cidr=...` 且该行高亮为选中（`2406:...:135:0/112 有线IPv6`）；点 `10.61.41.0/24` → `/#/dhcp/ledger/v4?cidr=10.61.41.0/24`；控制台无错误。
