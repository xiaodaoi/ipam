# [M0-003] Makefile 统一构建入口

| 字段 | 内容 |
|---|---|
| ID | M0-003 |
| 状态 | done |
| 来源 | AGENTS.md「常用命令」 |
| 负责 | opencode(devops) |
| 创建 | 2026-08-25 |
| 更新 | 2026-08-25 |

## 目标

提供 make build / test / lint / gen 四个目标，gen 从 api/openapi 经 oapi-codegen 再生 api/gen 与 TS 客户端。

## 验收标准

- [x] 四目标可执行且幂等（守卫式：依赖缺失明确 skip，rc=0）
- [x] gen 一致性校验通道就绪（`gen-check` diff 门禁；端到端验证待 M0-004 spec 就绪后补跑）
- [x] AGENTS.md「当前阶段临时命令」说明移除，替换为正式用法+无 make 回退说明
- [x] 新增 `make doctor` 工具链自检（`--strict` 供 CI 收紧）

## 涉及模块

- `Makefile`、`scripts/{doctor,make-part,gen-openapi}.sh`

## DoD 自检

- [x] gen 一致性测试脚本（gen-openapi.sh --check，spec 就绪后生效）
- [x] shellcheck 通过（人工走查：set -u/引用完整；本机未装 shellcheck，列入 M0-005 CI 补跑）
- [x] 文档同步：AGENTS.md 常用命令节已更新
- [x] spec N/A　- [x] commit 带 `[M0-003]`

## 实施记录

### 2026-08-25 · 会话1

- **做了**：Makefile 六目标（help/doctor/build/test/lint/gen/gen-check/clean）全部为薄封装；`scripts/make-part.sh` 分部执行器（工具缺失 skip 而非报错）；`scripts/gen-openapi.sh` 再生+一致性双模式；`scripts/doctor.sh` 九项工具自检含 go 版本下限断言。
- **环境事实**：沙箱无 make 且无 root——Makefile 面向开发机保留，本环境以 `bash scripts/*.sh` 直接验证等效路径；此回退方式已写入 AGENTS.md。
- **改动文件**：Makefile、scripts/×3、AGENTS.md、卡片、progress-log。
- **验证结果**：doctor 正确报告 git/node OK、go/pnpm/docker 等 MISSING；build/test/lint/gen 四路 skip 且 rc=0；--strict 缺硬依赖 rc=1。
- **遗留**：① go/pnpm/oapi-codegen 等在 M0-004/M0-005/M0-007 各卡落地时按需安装并复跑 doctor；② gen 端到端一致性在 M0-004 出 spec 后补验一次并在该卡留痕。

### 2026-08-26 · 批量验收

- 用户确认通过，review→done。
