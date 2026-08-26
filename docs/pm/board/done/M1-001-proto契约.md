# [M1-001] hook↔daemon gRPC 契约与代码生成

| 字段 | 内容 |
|---|---|
| ID | M1-001 | 状态 | done | 来源 | §2.1、D5 |
| 负责 | opencode(backend) | 创建/更新 | 2026-08-25 |

## 目标
落地 proto/coherence/v1/coherence.proto（ResolveBinding/ReportLease），protoc 生成 Go 侧桩（Makefile gen-proto），C++ 侧生成脚本留位。

## 验收标准
- [ ] go build 通过，消息字段与 §2.1 契约一致（mac/duid/subnet_id/hit/ipv6/source）
- [ ] scripts/gen-proto.sh 可重复执行（版本钉住 protoc 插件）

## DoD
单测 N/A / lint / 文档同步 §2.1 无偏差 / spec N/A / commit [M1-001]

## 实施记录

### 2026-08-25 · 会话1
- **做了**：coherence.proto（ResolveBinding/ReportLease+LeaseEvent 枚举）；scripts/gen-proto.sh（protoc36.0+插件钉版 v1.36.10/v1.6.2）；生成物入 proto/gen；Makefile gen-proto；grpc 依赖入 go.mod。
- **验证**：build ✓；枚举命名踩坑一次（双重前缀→改裸值名由生成器剥离）。

### 2026-08-26 · 批量验收

- 用户确认通过，review→done。
