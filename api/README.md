# api/ — 契约唯一事实源（D14 Spec-First）

- `openapi/`：手写 OpenAPI 3.1 spec（paths 按域拆分、components 含 securityScopes=权限码同源）
- `gen/`：oapi-codegen 生成物（Go 接口 + TS 客户端）——**禁止手改**，仅 `make gen` 再生

规范与 CI 门禁见架构文档 §12。
