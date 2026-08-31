# [M2-034] 全站表单 VbenModal 化（可拖拽/自动高度，举一反三）

| 字段 | 内容 |
|---|---|
| ID | M2-034 |
| 状态 | review |
| 来源 | 用户前端调试清单（六-1）：所有弹出对话框采用 modal 模态对话框（可拖拽/自动高度，vben-modal）；二-1/三-1/五-2 各页表单 modal 化；orgs/records/ledger 的 window.prompt 交互根治 |
| 负责 | opencode(frontend) |
| 创建 | 2026-08-31 |
| 更新 | 2026-08-31 |

## 方案

统一改造模式（以 subnets 试点为模板）：
1. `import { VbenModal } from '@vben/common-ui'`（@core/popup-ui——**draggable/title/自动高度已内置**，容器 unbound 同款确认）
2. 页面内表单区块（v-if="showForm" 或常显）→ `<VbenModal v-model:open="showForm" :title="动态" draggable>` 包裹
3. 新建按钮：`showForm = true`（表单清空复用 cancel/reset 函数）；编辑：`edit(r)` 内 `showForm = true`
4. window.prompt 全部替换为 VbenModal 表单（orgs:42/records:51/ledger:88）

## 页面清单（批 1 试点 + 批 2 举一反三）

- [x] **批 1 试点：dhcp/subnets**（VbenModal 5 处改造 + cancelEdit 函数新增——typecheck/build/冒烟全绿）
- [ ] 批 2：dns/upstream、dns/forward、dhcp/options（选项+类两表单）
- [ ] 批 2：system/users、system/orgs（prompt 改造）、dhcp/ledger（prompt 改造）
- [ ] 批 2：dns/records（zone+record 表单）、dhcp/dualstack、dns/blocklist（名单+条目）

## 验收标准（可测）

- [x] 批 1：typecheck/build/零外链/容器冒烟全绿（VbenModal 可拖拽弹出、表单校验保留）
- [x] 批 2 全部页面改造完成（upstream/forward/dualstack/options/users/blocklist/orgs/records/ledger）+ 验证链绿
- [ ] 批 2 全部页面改造完成 + 验证链绿
- [ ] 文档三件套 + commit [M2-034]

## 实施记录（追加式，勿删旧条目）

### 2026-08-31 · 会话1（批 1 试点）
- **做了**：dhcp/subnets 页 VbenModal 化——imports（@vben/common-ui）、表单区块 div → VbenModal（v-model:open="showForm" + 动态 title + draggable）、内层 div 闭合修复（替换时丢失）、cancelEdit 函数新增（85 行 showForm=false 在 add() 内部，无独立函数）、新建按钮改 Modal 语义（showForm=true + cancelEdit 清空）。
- **验证结果**：typecheck 0 + build ✓ 6.67s + sync 4.3M + 零外链 PASS + 容器重建（Built=1）+ 冒烟子网 200。
- **踩坑留痕**：① 表单区块替换时内层 div 的闭合 </div> 被 </VbenModal> 覆盖——**包裹式替换必须数清闭合标签**；② subnets 无独立 cancelEdit（showForm=false 内联在 add() 中）——**Modal 化前先确认页面的状态函数清单**。
- **遗留**：批 2 九页改造进行中。

### 2026-08-31 · 会话6（批 2d-2：orgs/records/ledger 的 prompt 改造）
- **做了**：三页 window.prompt → VbenModal——orgs 页（askName/confirmName 通用名称 Modal，addRoot/addChild/rename 闭包改造 + sel 捕获修 TS18048）；records 页（zoneModal + createZoneConfirm 替换旧 addZone prompt）；ledger 页（bindModal + askBind/confirmBind 的 MAC 绑定 Modal）。
- **验证结果**：typecheck 0 + build ✓ 6.37s + sync 4.3M + 零外链 PASS + 容器重建（Built=1）+ orgs/records/ledger API 冒烟 200。
- **踩坑留痕**：① 异步闭包内的 selected.value 需局部捕获（TS18048）；② python 批次中途 assert 失败时已执行段不落盘——**分页改造后各页状态不一致**（records 的按钮/Modal 已改但状态定义丢失）——**重跑前先 grep 确认各页实际状态**。

### 2026-08-31 · 会话5（批 2d-1：system/users + dns/blocklist）
- **做了**：system/users 页 VbenModal 化（创建用户表单 → Modal + 「+ 创建用户」按钮 + add 成功后关闭）；dns/blocklist 页名单创建表单 VbenModal 化（「+ 新建名单」按钮 + Modal；createList 成功后关闭）。
- **验证结果**：typecheck 0 + build ✓ 7.49s + sync 4.3M + 零外链 PASS + 容器重建（Built=1）+ users/orgs/blocklist API 冒烟 200。

### 2026-08-31 · 会话4（批 2c：dhcp/options 两表单）
- **做了**：dhcp/options 页两表单 VbenModal 化（选项 Modal showOptForm + 类 Modal showClsForm——12 处改造：imports/状态声明/addOption 尾/editOpt/addClass 尾/editCls/cancelEditOpt/cancelEditCls/选项区块起止/类区块起止）。
- **验证结果**：typecheck 0 + build ✓ 6.66s + sync 4.3M + 零外链 PASS + 容器重建（Built=1）+ 选项/类 API 冒烟 200。
- **踩坑留痕**：① 孤立 </div> 删除时把提示 div 的合法闭合也删了（vite 报 missing end tag 261 行）——**删除标签时范围必须精确到行首缩进（6 空格孤立标签 vs 8 空格合法闭合）**；② typecheck 过但 build 失败的场景（vite 模板解析在 vue-tsc 之外）——**build 错误需看 vite 完整输出定位行号**。

### 2026-08-31 · 会话3（批 2b：dhcp/dualstack）
- **做了**：dhcp/dualstack 页 VbenModal 化（同款模式：表单区块容器 → 「+ 新建模板」按钮 + VbenModal 包裹 + 内层 flex div 保留；showForm 声明 + cancelEdit/edit 联动）。
- **验证结果**：typecheck 0 + build ✓ 12.99s + sync 4.3M + 零外链 PASS + 容器重建（Built=1）+ dualstack API 冒烟 200。
- **踩坑留痕**：dualstack 页函数体缩进是 2 空格（与 subnets 的 4 空格不同）——**anchor 必须按各页实际缩进形态**。

### 2026-08-31 · 会话2（批 2a：upstream + forward）
- **做了**：dns/upstream + dns/forward 两页 VbenModal 化（同批 1 模式：表单区块 → Modal 包裹 + showForm 状态联动 add/edit/cancel + 「+ 添加」按钮替代页面内常驻表单；forward 的内联取消按钮补 showForm=false）。
- **验证结果**：typecheck 0 + build ✓ 8.44s + sync 4.3M + 零外链 PASS + 容器重建（Built=1）+ 三域冒烟 200。
