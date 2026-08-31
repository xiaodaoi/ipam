# [M2-037] Modal 不弹出修复（useVbenModal 命令式，举一反三全页面）

| 字段 | 内容 |
|---|---|
| ID | M2-037 |
| 状态 | review |
| 来源 | 用户调试发现：点击新建子网/添加上游等按钮无 Modal 弹出——M2-034 的 v-model:open 为错误用法（vben v5 的 Modal 显示控制是 modalApi.open() 命令式，modal-api.ts:97 close/158 open/171 setState 实锤） |
| 负责 | opencode(frontend) |
| 创建 | 2026-08-31 |
| 更新 | 2026-08-31 |

## 方案（useVbenModal 连接组件模式——vben 官方用法）

1. `import { useVbenModal } from '@vben/common-ui'`
2. `const [FormModal, formModalApi] = useVbenModal({ draggable: true, title: '...' })`
3. 打开：`formModalApi.setState({ title: '编辑子网' }); formModalApi.open()`；关闭：`formModalApi.close()`
4. 模板：`<FormModal class="w-[860px]">` 替代 `<VbenModal v-model:open=...>`；showForm ref 删除
5. setState 签名（modal-api.ts:171）：`setState(stateOrFn: ((prev) => Partial<ModalState>) | Partial<ModalState>)`——动态 title 走 setState

## 页面清单（举一反三）

- [x] 批 1 试点：dhcp/subnets（7 处改造，typecheck/build/零外链全绿）
- [x] 批 2：dns/upstream、dns/forward
- [x] 批 3：dhcp/dualstack、dhcp/options（两 Modal）、system/users、dns/blocklist
- [x] 批 4：system/orgs（名称 Modal）、dns/records（zone Modal）、dhcp/ledger（绑定 Modal）

## 验收标准（可测）

- [ ] 全部页面 useVbenModal 改造完成（v-model:open 清零）+ 验证链绿
- [ ] 用户浏览器确认 Modal 正常弹出
- [ ] 文档三件套 + commit [M2-037]

## 实施记录（追加式，勿删旧条目）

### 2026-08-31 · 会话1（批 1 试点：dhcp/subnets）
- **做了**：subnets 页 useVbenModal 改造 7 处（imports/useVbenModal 实例替代 showForm/add 尾 close/edit 内 setState+open/新建按钮 setState+open/模板 FormModal 开闭）。
- **验证结果**：typecheck 0 + build ✓ 7.59s + sync 4.3M + 零外链 PASS。
- **踩坑留痕**：① vben v5 的 Modal 无 v-model:open（ModalProps 无 open 字段）——显示控制唯一途径是 modalApi.open()/close()/setState()；② anchor 缩进先探再改（subnets 的 showForm.value 是 2 空格，4 空格锚点失配致首批 python 全部未落盘）。
- **遗留**：批 2/3/4 九页改造进行中。

### 2026-08-31 · 会话4（批 4：system/orgs + dns/records + dhcp/ledger 三页——全部收口）
- **做了**：三页 prompt/VbenModal → useVbenModal 命令式改造（orgs 6 处 + records 7 处 + ledger 6 处——Modal 实例（NameModal/ZoneModal/BindModal）/setState 动态标题/open/close/模板闭配对）。
- **验证结果**：typecheck 0 + build ✓ 6.72s + sync 4.3M + 零外链 PASS + go build/test 全绿 + lint 0 issues + 容器重建（Built=1）+ 11 端点冒烟全 200（子网/上游/转发/双栈/选项/用户/组织/名单/区域/台账/roles）。
- **踩坑留痕**：① records 的 VbenModal import 从未落盘（批 2d-2 失败批的 add_vben_import 随整段回滚）——**历史失败批要考古落盘缺口**；② show 残留散点（orgs 取消按钮/ledger 取消按钮）在声明改造后 TS2339 暴露——**show 字段删除后必须 grep 全文件残留**。
- **遗留**：无——**全部页面（10 页 13 Modal）useVbenModal 改造完成，v-model:open 清零**。

### 2026-08-31 · 会话3（批 3：dhcp/dualstack + dns/blocklist + system/users + dhcp/options）
- **做了**：四页 useVbenModal 命令式改造（dualstack 7 处 + blocklist 6 处 + users 6 处 + options 10 处——双 Modal 实例（OptModal/ClsModal）/闭标签配对/按钮 setState+open）。
- **验证结果**：typecheck 0 + build ✓ 6.53s + sync 4.3M + 零外链 PASS（options 的闭标签配对修复后）+ showOptForm/showClsForm 残留清零。
- **踩坑留痕**：① OptModal/ClsModal 开标签替换后 </VbenModal> 闭标签未配对（Invalid end tag 208 行）——**开闭标签必须成对替换**；② process 批量改造的中途失败不落盘（write 在循环后）——**分文件独立落盘 + 失败精确定位**。

### 2026-08-31 · 会话2（批 2：dns/upstream + dns/forward）
- **做了**：两页 useVbenModal 命令式改造（imports/Modal 实例替代 showForm 声明/add 尾 formModalApi.close/edit 内 setState+open/cancelEdit 内 close/模板 FormModal/按钮 setState+open——upstream 8 处 + forward 7 处 + forward 取消按钮内联 showForm → formModalApi.close()）。
- **验证结果**：typecheck 0 + build ✓ 6.61s + sync 4.3M + 零外链 PASS + Go 全链绿 + 容器重建（Built=1）+ 上游/转发 API 冒烟 200。
- **踩坑留痕**：① 取消编辑按钮的内联 showForm = false（模板内联——script 区改造漏）——**模板内联状态引用需一并排查**；② anchor 缩进按现场（4 空格/6 空格逐处确认）。
