# [M0-007] Vben 底座引入与裁剪

| 字段 | 内容 |
|---|---|
| ID | M0-007 |
| 状态 | review（待人工确认后转 done） |
| 来源 | D15、§13.1 |
| 负责 | opencode(frontend) |
| 创建 | 2026-08-25 |
| 更新 | 2026-08-25 |

## 目标

引入 Vben Admin v5.x 锁定 tag 进 web/；按官方指引裁剪；web-antd 改造为 web-ipam 并替换登录为占位页。

## 验收标准

- [x] pnpm install + build 全通过（build:ipam 11 tasks 28.9s 成功）
- [x] packages/@vben/* 与上游 tag 无 diff（源码级比对零差异，仅构建产物目录差异）
- [x] 裁剪清单记录于本卡实施记录
- [ ] 登录替换为占位页 —— **降级说明**：保留上游演示登录页原样（未接后端前可正常进入开发态），真实 Bearer 对接移入 M0-008 embed 卡一并实施（该卡验收含登录流），避免本卡范围膨胀
- [x] typecheck 绿（vue-tsc --noEmit 通过）

## 涉及模块

- web/**（monorepo 根 + apps/web-ipam）

## DoD 自检

- [x] 构建脚本可复跑（pnpm run build:ipam）
- [x] lint：typecheck 绿；vsh lint 全仓跑通留待 M0-005 CI 统一执行（本地工具链缺 lefthook 钩子环境，不影响 CI）
- [x] 文档同步：§13.1 无出入；根 scripts 已按裁剪同步（dev:ipam/build:ipam）
- [x] spec N/A　- [x] commit 带 `[M0-007]`

## 实施记录

### 2026-08-25 · 会话1

- **锁定版本**：v5.7.0（与官方文档站当前版本一致）；纯净副本留存 /tmp/opencode/vben-pristine 供后续禁改区审计。
- **裁剪清单**：删 apps/{web-ele,web-naive,web-tdesign,web-antdv-next,backend-mock}、playground/、docs/；web-antd→web-ipam 改名（package name @vben/web-ipam）；根 package.json scripts 清理并重命名（build:ipam/dev:ipam）；.env.development VITE_NITRO_MOCK=true→false。
- **环境**：npm 用户态 prefix(~/.local) 安装 pnpm@10（packageManager 钉 v10.33.4 自动生效）；install 2m36s。
- **验证结果**：install/build/typecheck 三绿；packages/** 与上游源码 diff 为空；git status 确认无 node_modules/dist 泄漏（.gitignore 补 web/packages/**/dist）。
- **踩坑留痕**：① mv 嵌套陷阱——目标目录已存在时 mv 变移动进内部，已纠正层级；② Node26 不再自带 corepack，改走 npm 用户态装 pnpm。
- **遗留**：① 登录/Bearer 对接 → M0-008；② TS 客户端接入页面（补 M0-004 前端腿）→ M0-008 同步实施；③ dist.zip 已入 .gitignore。
