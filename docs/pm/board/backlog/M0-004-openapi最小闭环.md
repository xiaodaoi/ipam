# [M0-004] OpenAPI 最小闭环（spec→Gin→TS→页面）

| 字段 | 内容 |
|---|---|
| ID | M0-004 |
| 状态 | backlog |
| 来源 | D14、§12.1/12.2 |
| 负责 | opencode(backend+frontend) |
| 创建 | 2026-08-25 |
| 更新 | 2026-08-25 |

## 目标

以 GET /api/v1/system/info 为样例打通三端：手写 spec → oapi-codegen 生成 Gin 接口与 TS 客户端 → control-plane 实现 → 前端页面调用展示。

## 验收标准

- [ ] spec 含 operationId/summary/description/双 example（§12.2 全要素示范）
- [ ] RFC 9457 错误模型组件定义并可复用
- [ ] 浏览器可见接口返回数据
- [ ] 此 spec 作为后续所有端点的模板范例

## 涉及模块

- api/openapi、api/gen、cmd/control-plane、internal/module/platform、web/apps/web-ipam

## DoD 自检

- [ ] handler 单测　- [ ] lint 绿　- [ ] 文档同步：§12 示例引用
- [ ] spec 先行 ✓（本卡即源头）　- [ ] commit 带 [M0-004]

## 实施记录

