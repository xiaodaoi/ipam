# [M2-030] DNSSEC 校验开关（B-10）

| 字段 | 内容 |
|---|---|
| ID | M2-030 |
| 状态 | review |
| 来源 | 架构文档 §571 P2 占位「DNSSEC 校验开关（P2，B-10）」——schema/持久化/handler 已由 M3-009 就绪，缺渲染（trust-anchor）与前端入口（Switch 灰置） |
| 负责 | opencode(backend+frontend) |
| 创建 | 2026-08-29 |
| 更新 | 2026-08-29 |

## 方案

1. BuildConf：`dnssecValidate=true` 时输出 `trust-anchor: ". IN DS 20326 8 2 E06D44B..."`（IANA 根锚 KSK-2017 公开常量——离线环境免 unbound-anchor 联网）+ 既有 val-permissive-mode 反相输出
2. settings_test：trust-anchor 断言
3. security 页：DNSSEC Switch 去灰置 + 文案（P2 → B-10）

## 范围说明

B-10 定义为**递归链路校验开关**（validator + 根锚）；权威区签名不在范围。

## 验收标准（可测）

- [x] 全链绿（go build/test/lint + typecheck + 零外链）
- [x] e2e：PUT dnssecValidate=true → unbound-rendered.conf 实收 trust-anchor + val-permissive-mode: no → false → yes + 锚消失
- [x] 文档三件套 + commit [M2-030]

## 实施记录（追加式，勿删旧条目）

### 2026-08-29 · 会话1
- **做了**：settings.go BuildConf trust-anchor 分支 + settings_test 断言 + security 页 Switch 去 disabled/文案更新；镜像重建部署。
- **验证结果**：go build/test/lint 全绿 + typecheck 0 + 零外链 PASS；e2e 决定性——PUT dnssecValidate=true → `/etc/unbound/unbound-rendered.conf` 实收 `val-permissive-mode: no` + `trust-anchor: ". IN DS 20326 8 2 E06D44B..."` → PUT false → `val-permissive-mode: yes` + 锚消失（开关双向闭环）。
- **踩坑留痕**：① control-plane 镜像 build 输出空行 ≠ 重建成功（缓存/静默）——Go 代码改动后必须 `grep Built` 确认再部署；② settings 渲染 conf 在 `/etc/unbound/unbound-rendered.conf`（M3-009 include 拆分）——grep 主 conf 无匹配 ≠ 渲染失败。
- **遗留**：无——**B-10 闭环**，架构文档最后一个 P2 功能块落地。
