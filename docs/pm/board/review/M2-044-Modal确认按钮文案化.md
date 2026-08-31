# [M2] 044-Modal确认按钮文案化

| 字段 | 内容 |
|---|---|
| ID | M2 |
| 状态 | review |
| 优先级 | P1 |
| 来源 | 用户清单：六-2：Modal底部确认按钮改为操作名（如添加），去掉内部操作按钮 |
| 负责 | opencode |
| 创建 | 2026-08-31 |

## 验收标准（可测）

- [x] 全站 12 个 Modal：footer 确认按钮带操作名 + onConfirm 回调 + body 操作按钮移除 + 验证链绿

## 实施记录（追加式，勿删旧条目）

### 2026-08-31 · 会话1（两批全站改造）
- **做了**：12 个 Modal 全部改造——① 实例化绑定 confirmText（创建（下发 Kea）/添加/创建模板/创建选项/创建类/创建名单/创建用户/确定/创建/绑定）+ onConfirm 回调；② 编辑场景 setState 动态 confirmText: '保存修改'；③ Modal body 内操作按钮全部移除（footer 承担），orgs/ledger 的自定义按钮区整体删除。
- **验证结果**：typecheck 0 + build ✓ 6.44s + sync 4.3M + 零外链 PASS + 容器重建 + 冒烟 200。
- **踩坑留痕**：setState 正则替换时 title 引号被 confirmText 拼接吃掉（TS1005）——字符串拼接替换必须整行重写。
- **遗留**：无——M2-044 完成。roles 页无 Modal 不涉及。
