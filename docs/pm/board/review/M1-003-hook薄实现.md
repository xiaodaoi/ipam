# [M1-003] libcoherence.so 薄 C++ hook

| 字段 | 内容 |
|---|---|
| ID | M1-003 | 状态 | doing | 来源 | §2.1、K4(<800行) |
| 负责 | opencode(backend) | 创建/更新 | 2026-08-25 |

## 目标
pkt6_receive 提取 MAC/DUID→同步 ResolveBinding(UDS,50ms超时)；lease6_select 注入地址；降级读 bindings.snapshot。

## 验收标准
- [ ] CMake 构建通过（本地无 Kea 头文件则 mock 头编译）；fuzz 用例入 tests/fuzz
- [ ] 行数统计 <800 行硬门禁进 CI

## DoD
fuzz 测试 / C++ 编译绿 / K4 记录 risks / N/A / commit [M1-003]

## 实施记录

### 2026-08-25 · 会话1
- **做了**：核心库 coherence_lib（MAC 归一化/快照行协议 v2 解析/查找）零依赖纯 std；hook_entry.cc Kea 胶水（pkt6_receive/lease6_select/load/unload，IPAM_HAVE_KEA 宏隔离）；CMake（core 常编译、hook 模块按 KEA_HEADERS_DIR、FUZZ 选项）；ctest 测试 12 断言；fuzz_snapshot.cc（K4 强制项，Clang 环境构建）；行数门禁脚本 150/800 ✓；CI 新增 hook-ci job。
- **联动变更**：Go 快照格式 v2 切换为管道行协议（两侧自有，C++ 零依赖解析）；对应 Go 单测同步更新仍绿。
- **严谨性取舍（如实记录）**：① gRPC C++ 客户端未实现——本地无 grpc 头无法验证，臆测代码已删除而非提交；正式实现移入 M1-005（真机 Kea+grpc 环境），当前 hook 走 §2.1 快照降级路径（合法主路径之一）；② proto/gen/cpp 已预生成 pb 基础件备 M1-005 使用。
