# [M1-004] bindings.snapshot 快照与 PG 对账

| 字段 | 内容 |
|---|---|
| ID | M1-004 | 状态 | doing | 来源 | §2.1降级策略、K9 |
| 负责 | opencode(backend) | 创建/更新 | 2026-08-25 |

## 目标
daemon 每5s刷新只读快照至 /run/ipam/bindings.snapshot；PG LISTEN/NOTIFY 触发增量 + 启动全量加载；local_data 重放对账任务同卡实现。

## 验收标准
- [ ] 快照原子写(tmp+rename)；对账任务幂等可重跑
- [ ] 集成测试：NOTIFY→缓存更新→快照变化 ≤5s

## DoD
集成测试 / lint / §2.3 同步 / N/A / commit [M1-004]

## 实施记录

### 2026-08-25 · 会话1
- **做了**：snapshot.go（tmp+rename 原子写+5s 循环）；pgstore.go（LoadAllBindings 全量重放+SubscribeNotify 断线重连订阅）；unbound.go（§4.4 四条 RR 生成：A/AAAA/双向 PTR，MAC 缺省主机名规则）与 unbound_ctl.go（ExecController unbound-control 下发+Reconciler 差分对账，失败保留状态待重试）；0002_notify.sql 触发器；daemon 接线 -dsn/-snapshot/-zone。
- **验证**：13 单测全绿（快照往返/无 tmp 残留/50ms 刷新/四 RR/差分幂等/失败重试/换址增删/unavailable 语义）；build/vet/golangci-lint 0 issues。
- **踩坑留痕**：keyOf 取 TYPE 应为 f[3]；v4 PTR 反转断言笔误——均测试先行暴露后修复。
- **遗留（如实）**：live PG LISTEN/NOTIFY 与 unbound-control 真实下发验证需 M1-005 环境；grace 状态机 M2。

### 2026-08-26 · 批量验收

- 用户确认通过，review→done。
