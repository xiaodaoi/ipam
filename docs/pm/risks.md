# 风险登记簿

> 源自架构文档 §10（K1~K9）。每里程碑出口强制复盘一次：状态 ∈ open / mitigating / closed；"最近复盘"填日期。

| # | 风险 | 跟进动作 | 状态 | 最近复盘 |
|---|---|---|---|---|
| K1 | Unbound local-data/auth-zone 优先级未实测 | PoC 用例纳入 M1 | open | — |
| K2 | Kea 升级可能重建 PG 租约表导致触发器丢失 | 迁移脚本幂等化 + 升级检查项 | open | — |
| K3 | 主备各自 PG 的配置漂移 | config-replicate 幂等 + 定时对账任务 | open | — |
| K4 | C++ hook 维护性（团队少量 C++） | hook 保持 <800 行；CI 强制 fuzz | open | — |
| K5 | Android SLAAC 终端覆盖率话术 | 按 D-08 输出卖前说明页 | open | — |
| K6 | view 内嵌 rpz 需 Unbound ≥1.16 版本行为差异 | M3 固化用例；不满足则降级多实例兜底 | open | — |
| K7 | DoH/私改 DNS 绕过封禁 | DoH 域名/IP 出厂入黑名单 + NAT 重定向样例 | open | — |
| K8 | jammy 系统源 unbound=1.13.1 不满足 ≥1.16 | 镜像用 NLnetLabs 官方源固版，CI 断言版本号 | open | — |
| K9 | local_data 运行态变更重启丢失 | daemon 对账：启动全量重放 + 周期增量比对 | open | — |

新增风险按 K10 起编号追加；关闭需附证据。
