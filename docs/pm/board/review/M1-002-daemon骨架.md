# [M1-002] coherence-daemon 骨架（UDS gRPC + 内存绑定缓存）

| 字段 | 内容 |
|---|---|
| ID | M1-002 | 状态 | doing | 来源 | §4.3、§8(P99≤5ms) |
| 负责 | opencode(backend) | 创建/更新 | 2026-08-25 |

## 目标
cmd/coherence-daemon：监听 /run/ipam/coherence.sock；ResolveBinding 实现 B 型映射计算（{v4.hextet4}）+ 内存热集缓存；ReportLease 异步落内存台账。

## 验收标准
- [ ] bufconn 单测：hit/compute/none 三路径；B 型样例 10.61.172.10→2406::10:61:172:10
- [ ] 模板 expr 解析支持 B/A 两型（A 型 hex32）

## DoD
单测覆盖映射算法 / vet+lint / §4.3 如有出入先改文档 / N/A / commit [M1-002]

## 实施记录

### 2026-08-25 · 会话1
- **做了**：mapping.go B/A 型映射算法（含非法输入与 CUSTOM 拒绝）；service.go ResolveBinding 三态(NONE→COMPUTED→CACHE)+ReportLease 生命周期；MemStore；cmd/coherence-daemon UDS 入口。
- **验证**：5 单测全绿（§4.3 样例 10.61.172.10→2406::10:61:172:10 与 A 型 a3d:ac0a 均断言）；build/vet/golangci-lint 0 issues。
- **遗留**：PG 接线(M1-004)；grace 状态机与冲突检测(M2)；P99 压测在 M1 真机阶段。
