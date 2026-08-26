# [M3-006] unbound 引擎编排器整合

| ID | M3-006 | 状态 | review | 来源 | §2.3、K1/K9 |
|---|---|---|---|---|---|
| 负责 | opencode(backend) | 创建 | 2026-08-26 |

## 目标
统一 unbound config 生成器（上游+转发+解析记录+封禁+参数五源合成）、checkconf 门禁、reload/auth_zone_reload 通道、local_data 对账接入 daemon（K9 实证）。

## 验收标准
- [ ] 五源合成后 checkconf 通过（CI 用容器实测）
- [ ] local-data 优先级 vs auth-zone 冲突用例（K1）

## DoD
集成测试 / lint / §2.3/§10 同步 / N/A / commit [M3-006]

## 实施记录

### 2026-08-26 · 会话1
- **做了**：engine/unbound 五源合成 BuildConf（参数段+默认转发+条件规则+auth-zone+RPZ 引用）；CheckConf 真实现（候选全文写临时文件→unbound-checkconf 校验）；confApplier 装配（渲染→checkconf→原子落盘→reload）+ POST /dns/conf/apply 端点；K1 实证资产落地（unbound.conf 增 auth-zone corp.local. + overlap 同名冲突记录 + auth-only 孤立记录，CI 断言 local-data 优先）；control-plane 镜像内嵌同版本 unbound（checkconf/客户端对齐）。
- **验证**：engine/unbound 5 单测全绿（五源合成断言/空骨架/缺二进制语义）；CI compose-smoke 含 K1 新断言（运行结果异步确认，不阻塞推进）。
- **踩坑留痕**：python heredoc 写 Go 字符串时 \n 转义陷阱；多轮脚本叠加导致 YAML 缩进混乱——最终以「整块重写」取代碎片修补。
- **遗留（如实）**：① 跨容器 conf 共享卷设计（cp 渲染→unbound 读）在 M4 网络存储任务落地，apply 端点已就绪待接线；② K9 对账实证已有单测覆盖（daemon 重放），真机复测随 §9；③ forward_remove 差量 P1。
