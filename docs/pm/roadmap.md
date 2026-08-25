# 项目路线图

> 里程碑对齐架构文档 D12（最大风险优先）与需求报告 §15（MVP=P0 集）。出口条件即验收门槛；进度 = 已完成任务卡 / 总任务卡。

| 里程碑 | 内容 | 出口条件（验收门槛） | 状态 | 进度 |
|---|---|---|---|---|
| M0 地基 | git/目录骨架/OpenAPI 闭环/CI 六道闸/compose 骨架/Vben 引入/embed 打通 | compose 冒烟 ✓ 全闸绿；spec→Gin→embed→docker 贯通（4 卡 review 待批量归档） | 基本完成 | 100% |
| M1 联动 PoC | hook-coherence + coherence-daemon + Kea PG 后端 + unbound local_data 推送 | §9：100 终端竞态联动 ≥99.9%，ResolveBinding P99≤5ms | 未启动 | 0% |
| M2 DHCP+IPAM | 子网/池 CRUD、组织树、地址台账六态、保留与绑定、资产登记 | FR-A/C P0 验收通过；台账着色与租约明细正确 | 未启动 | 0% |
| M3 DNS 全量 | 上游管理/转发规则/解析记录/RPZ 封禁/策略分组/探活 prober | §9：RPZ 50 万条 ≤60s 加载、view 分组隔离正确 | 未启动 | 0% |
| M4 日志+仪表盘 | vector→ClickHouse、检索 API、实时流、仪表盘卡片 | E-04：组合查询 5000 终端一天数据 ≤3s | 未启动 | 0% |
| M5 平台硬化交付 | RBAC/License/HA 主备/config-replicate/compose+helm 一键 | §9 清单全项通过；裸机 ≤30min 出服务 | 未启动 | 0% |

里程碑出口评审动作：
1. 核对出口条件逐条打勾并留证据链接（测试输出/截图）；
2. 在 `docs/dev/M{n}-实现说明.md` 沉淀实现说明；
3. 复盘 `risks.md` 全部开放风险；
4. 更新本表状态与进度。
