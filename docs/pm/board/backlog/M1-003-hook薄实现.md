# [M1-003] libcoherence.so 薄 C++ hook

| 字段 | 内容 |
|---|---|
| ID | M1-003 | 状态 | backlog | 来源 | §2.1、K4(<800行) |
| 负责 | opencode(backend) | 创建/更新 | 2026-08-25 |

## 目标
pkt6_receive 提取 MAC/DUID→同步 ResolveBinding(UDS,50ms超时)；lease6_select 注入地址；降级读 bindings.snapshot。

## 验收标准
- [ ] CMake 构建通过（本地无 Kea 头文件则 mock 头编译）；fuzz 用例入 tests/fuzz
- [ ] 行数统计 <800 行硬门禁进 CI

## DoD
fuzz 测试 / C++ 编译绿 / K4 记录 risks / N/A / commit [M1-003]
