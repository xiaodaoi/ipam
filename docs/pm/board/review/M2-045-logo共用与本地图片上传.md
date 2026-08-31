# [M2] 045-logo共用与本地图片上传

| 字段 | 内容 |
|---|---|
| ID | M2 |
| 状态 | review |
| 优先级 | P2 |
| 来源 | 用户清单：五-2：favicon与侧栏logo共用单图标+本地图片上传 |
| 负责 | opencode |
| 创建 | 2026-08-31 |

## 验收标准（可测）

- [x] 上传区（预览/清除/200KB 限制）+ dataURL 存 settings（favicon=logo 共用）+ e2e 大 payload 200

## 实施记录（追加式，勿删旧条目）

### 2026-08-31 · 会话1（共用图标 + 本地上传）
- **做了**：① 设置页两个 URL 输入框合并为「站点图标（页签+侧栏共用）」本地文件上传区（预览 + 清除 + 200KB 限制 + FileReader→dataURL，同时写入 faviconUrl/logoUrl）；② spec 的 URL 字段 maxLength 512→200000 + gen 重跑（base64 dataURL 存 settings 单行表）。
- **验证结果**：typecheck 0 + build ✓ 7.60s + sync 4.3M + 零外链 PASS + 容器重建 + e2e：PUT 13.6KB dataURL=200、GET 回读 13662 字符。
- **踩坑留痕**：favicon link href 支持 dataURL，保存后即页签生效；侧栏 logo 的运行时替换属 vben 布局层（禁改区），logoUrl 已存储、展示接入放后续卡。
- **遗留**：无——M2-045 完成。
