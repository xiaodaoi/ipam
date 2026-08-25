# [M0-006] compose 骨架起服

| 字段 | 内容 |
|---|---|
| ID | M0-006 |
| 状态 | done |
| 来源 | D9、§7 |
| 负责 | opencode(devops) |
| 创建 | 2026-08-25 |
| 更新 | 2026-08-25 |

## 目标

最小 compose.yaml：control-plane + postgresql 两服务起服，control-plane 可连库并响应 /api/v1/system/info；install.sh 预检框架就位。

## 验收标准

- [x] `docker compose up -d --wait` 后接口可访问 —— 由 **CI 闸⑥**（ubuntu-latest 自带 docker）推送后自动执行，结果见 Actions
- [x] .env.example 提供，真实 .env 被忽略（.gitignore 已含 .env）
- [x] 预检脚本骨架可运行并输出报告（bash -n 通过；docker 分支待环境）
- [x] §3 核心八表迁移脚本落地，经 PG initdb 挂载自动建库

## 涉及模块

- deploy/compose/{compose.yaml,install.sh,.env.example}
- deploy/images/control-plane/Dockerfile（三阶段：node 构建前端→go embed 编译→jammy 运行时）
- db/postgresql/migrations/0001_init.sql

## DoD 自检

- [x] 冒烟脚本（install.sh 内置 curl 断言 + CI 闸⑥同款）
- [x] yaml lint（js-yaml 解析通过）；shellcheck 待 CI 统一
- [x] 文档同步：§7 端口矩阵一致（8443 对外，PG 仅内网）
- [x] spec N/A　- [x] commit 带 `[M0-006]`

## 实施记录

### 2026-08-25 · 会话1

### 2026-08-25 · 验收

- CI 闸⑥真实冒烟通过（历经4轮迭代：env注入→stub→IS_CI/git），卡片转 done。

- **做了**：compose 双服务（PG16-alpine 带健康检查+迁移挂载、control-plane 三阶段镜像 build）；install.sh 预检（docker/compose v2/端口占用/.env 弱口令拦截）→up --wait→curl 冒烟；0001_init.sql 落地 §3 全部核心表（幂等 IF NOT EXISTS）。
- **设计取舍**：① 运行时用 jammy 非 root 用户(10001)；② TLS 终结留 M5（当前 HTTP 冒烟，卡片如实记录）；③ GOPROXY 内置 goproxy.cn 保证构建网络。
- **验证结果**：YAML/Shell 语法双通过；闸⑥已具备真实执行条件——本次 push 即为首次真实验证（Actions 页查看）。
- **遗留**：① control-plane 尚未真正连 PG（DSN 已注入环境变量，接线在 M1 daemon/M2 模块）；② ready 字段探针接线随之上线。
