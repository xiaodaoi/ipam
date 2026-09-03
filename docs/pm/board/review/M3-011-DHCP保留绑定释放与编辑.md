# M3-011 DHCP 保留/绑定释放与编辑 + 台账地图交互优化

## 目标（用户五项反馈）

1. 地址台账菜单移到子网管理下第二位
2. 保留列表支持释放 → 地址回归正常可下发状态（保留与绑定页 + 台账地图）
3. 静态绑定列表支持编辑（IP/MAC）与释放 → 释放后可动态下发
4. 台账地址规划编辑弹框过大（铺满全屏）→ 缩小
5. 「转动态」语义混乱 → 改为「释放」；释放后地址状态与前端颜色实时刷新

## 实施记录

### API（spec-first）

- `api/openapi`：新增 `POST /ledger/release`（ReleaseRequest{address}，404=无记录）与 `PUT /ledger/bind`（UpdateBindingRequest{address,mac}，400=MAC 非法、404=无记录）；`make gen` 再生
- 后端：`ReservationRepo` 增加 `UpdateMAC`（Mem 覆写 / PG UPDATE+RowsAffected 判 404——PG Upsert 是 ON CONFLICT DO NOTHING，不能复用）；`LedgerService.Release`（findReserved→Delete→notifyApply，在线租约不受影响）与 `UpdateBinding`（NormalizeMAC 校验 17 位→UpdateMAC→notifyApply）；保留↔绑定互转同语义（写 MAC 即成绑定）
- 单测：`TestLedgerService_Release_释放回归可下发`、`TestLedgerService_UpdateBinding_改绑与保留互转`（含 MAC 规范化/BAD_MAC/404 分支）

### 前端

- 菜单：`dhcp.ts` Ledger 移至 DhcpSubnets 之后（order=子项数组序）
- 保留与绑定页：两列表加操作列（fixed right）——保留行[释放]、绑定行[编辑/释放]；释放走 `Modal.confirm`（说明在线租约不受影响）；编辑弹窗改 MAC（updateBinding）或改地址（释放旧+绑定新，需行有 subnetId）
- 台账地图（IpPlanMap）：「转动态」按钮改为危险样式「释放」，emit `toRelease`；ledger 页逐地址调 releaseAddress 后 `loadMap()` 刷新（状态/颜色随之回归 available 灰蓝）；批量转保留/释放均逐地址循环（修原"仅处理首个但提示 N 个"的不诚实行为）；组件层 success 消息移除，由父层按 API 结果提示
- 弹窗尺寸根因：`width="540"` 传字符串 → antd 应用为无效 CSS → 宽度 auto 铺满全屏。改数字绑定 `:width="520"`（编辑弹窗）/`:width="420"`（详情抽屉），并瘦身表单（移除租约开始/到期——租约数据由 DHCP 驱动只读，编辑是假操作）

### 验证（Playwright 全流程）

- 菜单顺序：子网管理 → **地址台账** → 保留与绑定 ✓
- 地图：转保留 `#9DBEFF→#FF9C6E`、释放 `#FF9C6E→#9DBEFF`（状态颜色实时刷新）✓
- 绑定 host61 → `#69C0FF`；重复绑定 409 占用检查 ✓
- 编辑 MAC：PUT 204 → 列表 ee:99 ✓；释放：确认弹窗 → POST 204 → 行消失 → 地图回归灰蓝 ✓
- 编辑弹窗实测 520×626（1600 视口），不再铺满 ✓

### 已知取舍

- 批量转保留/释放逐地址串行（/24 规模 ≤253 次，可接受；后续可上批量端点）
- 保留改地址 = 释放+重绑两步（非事务）；bulk 端点仅创建语义，未扩展
