# [M0-007] Vben 底座引入与裁剪

| 字段 | 内容 |
|---|---|
| ID | M0-007 |
| 状态 | backlog |
| 来源 | D15、§13.1 |
| 负责 | opencode(frontend) |
| 创建 | 2026-08-25 |
| 更新 | 2026-08-25 |

## 目标

引入 Vben Admin v5.x 锁定 tag 进 web/；按官方指引裁剪：删 apps/web-ele、web-naive、playground、docs、backend-mock；web-antd 改名 web-ipam 并替换登录为占位页。

## 验收标准

- [ ] pnpm install + dev/build 全通过
- [ ] packages/@vben/* 与上游 tag 无 diff（禁改区基线确立）
- [ ] 裁剪清单记录于本卡实施记录

## 涉及模块

- web/**、web/apps/web-ipam/

## DoD 自检

- [ ] 构建脚本可复跑　- [ ] oxlint/eslint 绿
- [ ] 文档同步：§13.1 若上游流程有出入先修订文档
- [ ] spec N/A　- [ ] commit 带 [M0-007]

## 实施记录

