# [M2-016] DHCP 选项与类匹配页

| 字段 | 内容 |
|---|---|
| ID | M2-016 |
| 状态 | review |
| 来源 | 架构文档 §561（DHCP 菜单 5/6）：标准选项配置（C-02）、类匹配规则（C-03） |
| 负责 | opencode(backend/frontend) |
| 创建 | 2026-08-28 |
| 更新 | 2026-08-28 |

## 目标

DHCP 菜单第 5 项落地：全局标准选项（C-02）与类匹配规则（C-03，Kea client-classes）CRUD；kea BuildConfig 注入 option-data 与 client-classes，变更后 config-set 原子下发（软失败语义）。启用规划中的 internal/module/dhcp 模块。

## 验收标准（可测）

- [ ] spec /dhcp/options + /dhcp/classes CRUD（闸① 0 errors）+ gen；tag=dhcp
- [ ] 迁移 0011：dhcp_options（UNIQUE(option_code,name)）、dhcp_classes（name UNIQUE，options jsonb）
- [ ] kea BuildConfigFull(subnets, opts, classes) 生成含 client-classes/option-data（单测锚定 JSON）；既有 BuildConfig 委托保持兼容
- [ ] 变更后触发下发；kea 不可达时软失败（X-Kea-Warning 头，CRUD 仍 2xx）——对齐 M3-001 语义
- [ ] 前端 DHCP 菜单「选项与类匹配」页（选项表+类表双卡 CRUD）；typecheck/零外链 PASS
- [ ] 容器 e2e：建类+选项 → config-set 成功（kea 在线）或软失败结构化；列表回读一致
- [ ] lint 0、commit [M2-016]

## 涉及模块

- `api/openapi/{paths/dhcpoption.yaml,components/schemas/dhcpoption.yaml,openapi.yaml}`
- `internal/module/dhcp/`（启用空目录：types/store/handler）
- `internal/engine/kea/config.go`（BuildConfigFull 扩展）
- `db/postgresql/migrations/0011_dhcp_options.sql`
- `web/apps/web-ipam/src/{api/ipam.ts,views/dhcp/options/index.vue}` + dhcp 路由/语言包

## 实施记录（追加式，勿删旧条目）

### 2026-08-28 · 会话1
- **做了**：spec /dhcp/options + /dhcp/classes CRUD（tag dhcp；3 处缺 description 闸①报错补齐；0 errors）+ gen；迁移 0011 dhcp_options/dhcp_classes（**被 M5-005 runner 自动应用——首次实战验证增量迁移**）；启用 internal/module/dhcp（types/store Mem/Pg jsonb + handler 8 方法）；kea BuildConfigFull（全局 option-data + client-classes 注入，disabled 剔除，空不注键，既有 BuildConfig 兼容）；main 装配（keaCmd + applyDhcp 闭包：subRepo.List→BuildConfigFull→config-set，dhcpAPI 包装）；前端 DHCP 菜单「选项与类匹配」双卡页（选项 CRUD+类 CRUD 动态行）。
- **验证结果**：全链测试绿（kea 3 组/platform/control-plane）+ lint 0 + typecheck 0 + 零外链 PASS；容器 e2e——**合法配置被 Kea 原子接受**（PATCH printers.test → 200 无 warning，kea 无错误日志），CRUD 201/200/204 + 回读一致，软失败语义全程生效（非法配置 X-Kea-Warning + 数据落库）。
- **踩坑留痕（kea 下发三连 bug，全部修复）**：① Command 传 cfg.Dhcp4 缺外层包裹 → "Missing mandatory 'Dhcp4' parameter"——kea arguments 需完整顶层 {Dhcp4:{...}}，**ctrl.go RealApply 同源既有 bug 一并修复**（真实 kea 上子网下发此前也会失败，被 fake 掩盖）；② option-data 只带 name → 非标准名 Kea 查表报 code '0'——改发 code+data（name 仅 DB/API 展示层）；③ 示例表达式含 matches 操作符——**Kea 2.0+ 已移除**（PCRE 遗产），改为 option[61].hex == option[61].hex，spec/页面/单测锚点同步。诊断期间软失败吞错误无日志可查 → 闭包加 [dhcp-apply] 日志（沿 [migrator] 惯例）。
- **运维事件**：多次镜像构建堆满磁盘（ENOSPC，postgres unhealthy 同根因）→ docker builder/image prune 释放 58G；postgres 自愈。
- **遗留**：Kea 自定义选项定义（code 224+ 需 option-def）P2；test 表达式合法性预校验（前端提示 Kea eval 语法）P2。
