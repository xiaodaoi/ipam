# [M0-006] compose 骨架起服

| 字段 | 内容 |
|---|---|
| ID | M0-006 |
| 状态 | backlog |
| 来源 | D9、§7 |
| 负责 | opencode(devops) |
| 创建 | 2026-08-25 |
| 更新 | 2026-08-25 |

## 目标

最小 compose.yaml：control-plane + postgresql 两服务起服，control-plane 可连库并响应 /api/v1/system/info；install.sh 预检框架就位（端口/内核参数占位）。

## 验收标准

- [ ] `docker compose up -d` 后接口可访问
- [ ] .env.example 提供，真实 .env 被忽略
- [ ] 预检脚本骨架可运行并输出报告

## 涉及模块

- deploy/compose/、deploy/images/control-plane/、db/postgresql/migrations/

## DoD 自检

- [ ] 冒烟脚本　- [ ] yaml lint
- [ ] 文档同步：§7 端口矩阵核实　- [ ] spec N/A　- [ ] commit 带 [M0-006]

## 实施记录

