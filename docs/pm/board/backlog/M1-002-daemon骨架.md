# [M1-002] coherence-daemon 骨架（UDS gRPC + 内存绑定缓存）

| 字段 | 内容 |
|---|---|
| ID | M1-002 | 状态 | backlog | 来源 | §4.3、§8(P99≤5ms) |
| 负责 | opencode(backend) | 创建/更新 | 2026-08-25 |

## 目标
cmd/coherence-daemon：监听 /run/ipam/coherence.sock；ResolveBinding 实现 B 型映射计算（{v4.hextet4}）+ 内存热集缓存；ReportLease 异步落内存台账。

## 验收标准
- [ ] bufconn 单测：hit/compute/none 三路径；B 型样例 10.61.172.10→2406::10:61:172:10
- [ ] 模板 expr 解析支持 B/A 两型（A 型 hex32）

## DoD
单测覆盖映射算法 / vet+lint / §4.3 如有出入先改文档 / N/A / commit [M1-002]
