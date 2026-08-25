# [M1-004] bindings.snapshot 快照与 PG 对账

| 字段 | 内容 |
|---|---|
| ID | M1-004 | 状态 | backlog | 来源 | §2.1降级策略、K9 |
| 负责 | opencode(backend) | 创建/更新 | 2026-08-25 |

## 目标
daemon 每5s刷新只读快照至 /run/ipam/bindings.snapshot；PG LISTEN/NOTIFY 触发增量 + 启动全量加载；local_data 重放对账任务同卡实现。

## 验收标准
- [ ] 快照原子写(tmp+rename)；对账任务幂等可重跑
- [ ] 集成测试：NOTIFY→缓存更新→快照变化 ≤5s

## DoD
集成测试 / lint / §2.3 同步 / N/A / commit [M1-004]
