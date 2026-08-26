# [M3-006] unbound 引擎编排器整合

| ID | M3-006 | 状态 | doing | 来源 | §2.3、K1/K9 |
|---|---|---|---|---|---|
| 负责 | opencode(backend) | 创建 | 2026-08-26 |

## 目标
统一 unbound config 生成器（上游+转发+解析记录+封禁+参数五源合成）、checkconf 门禁、reload/auth_zone_reload 通道、local_data 对账接入 daemon（K9 实证）。

## 验收标准
- [ ] 五源合成后 checkconf 通过（CI 用容器实测）
- [ ] local-data 优先级 vs auth-zone 冲突用例（K1）

## DoD
集成测试 / lint / §2.3/§10 同步 / N/A / commit [M3-006]
