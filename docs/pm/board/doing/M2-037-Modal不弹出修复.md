# [M2-037] Modal 不弹出修复（useVbenModal 命令式，举一反三全页面）

| 字段 | 内容 |
|---|---|
| ID | M2-037 |
| 状态 | doing |
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
- [ ] 批 2：dns/upstream、dns/forward
- [ ] 批 3：dhcp/dualstack、dhcp/options（两 Modal）、system/users、dns/blocklist
- [ ] 批 4：system/orgs（名称 Modal）、dns/records（zone Modal）、dhcp/ledger（绑定 Modal）

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
