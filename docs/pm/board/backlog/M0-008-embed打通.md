# [M0-008] embed 打通（前端产物 → :8443）

| 字段 | 内容 |
|---|---|
| ID | M0-008 |
| 状态 | backlog |
| 来源 | §13.3、架构图 L34 |
| 负责 | opencode(backend+frontend) |
| 创建 | 2026-08-25 |
| 更新 | 2026-08-25 |

## 目标

web-ipam 构建产物输出到 cmd/control-plane/webui/dist 并 go:embed；Gin SPA fallback（NoRoute→index.html）；浏览器访问 :8443 出完整页面并调通 M0-004 接口。

## 验收标准

- [ ] 单二进制运行，无外部静态文件依赖
- [ ] 刷新深层路由不 404（fallback 生效）
- [ ] scripts/ 内零外链资产断言脚本就位并入 CI

## 涉及模块

- cmd/control-plane/webui/、internal/（路由注册）、scripts/

## DoD 自检

- [ ] fallback 单测　- [ ] lint 绿
- [ ] 文档同步：§13.3 命令路径核实　- [ ] spec N/A　- [ ] commit 带 [M0-008]

## 实施记录

