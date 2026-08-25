# [M0-003] Makefile 统一构建入口

| 字段 | 内容 |
|---|---|
| ID | M0-003 |
| 状态 | backlog |
| 来源 | AGENTS.md「常用命令」 |
| 负责 | opencode(devops) |
| 创建 | 2026-08-25 |
| 更新 | 2026-08-25 |

## 目标

提供 make build / test / lint / gen 四个目标，gen 从 api/openapi 经 oapi-codegen 再生 api/gen 与 TS 客户端。

## 验收标准

- [ ] 四目标可执行且幂等
- [ ] gen 输出与已提交的 api/gen 完全一致（diff 为空）
- [ ] AGENTS.md 中"当前阶段临时命令"说明移除

## 涉及模块

- Makefile、api/gen、scripts/

## DoD 自检

- [ ] gen 一致性测试脚本　- [ ] shellcheck 通过
- [ ] 文档同步：AGENTS.md　- [ ] spec N/A　- [ ] commit 带 [M0-003]

## 实施记录

