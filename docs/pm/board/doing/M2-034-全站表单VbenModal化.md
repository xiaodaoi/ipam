# [M2-034] 全站表单 VbenModal 化（可拖拽/自动高度，举一反三）

| 字段 | 内容 |
|---|---|
| ID | M2-034 |
| 状态 | doing |
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
- [ ] 批 2 全部页面改造完成 + 验证链绿
- [ ] 文档三件套 + commit [M2-034]

## 实施记录（追加式，勿删旧条目）

### 2026-08-31 · 会话1（批 1 试点）
- **做了**：dhcp/subnets 页 VbenModal 化——imports（@vben/common-ui）、表单区块 div → VbenModal（v-model:open="showForm" + 动态 title + draggable）、内层 div 闭合修复（替换时丢失）、cancelEdit 函数新增（85 行 showForm=false 在 add() 内部，无独立函数）、新建按钮改 Modal 语义（showForm=true + cancelEdit 清空）。
- **验证结果**：typecheck 0 + build ✓ 6.67s + sync 4.3M + 零外链 PASS + 容器重建（Built=1）+ 冒烟子网 200。
- **踩坑留痕**：① 表单区块替换时内层 div 的闭合 </div> 被 </VbenModal> 覆盖——**包裹式替换必须数清闭合标签**；② subnets 无独立 cancelEdit（showForm=false 内联在 add() 中）——**Modal 化前先确认页面的状态函数清单**。
- **遗留**：批 2 九页改造进行中。
