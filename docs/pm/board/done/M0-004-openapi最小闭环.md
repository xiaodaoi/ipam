# [M0-004] OpenAPI 最小闭环（spec→Gin→TS→页面）

| 字段 | 内容 |
|---|---|
| ID | M0-004 |
| 状态 | done |
| 来源 | D14、§12.1/12.2 |
| 负责 | opencode(backend+frontend) |
| 创建 | 2026-08-25 |
| 更新 | 2026-08-25 |

## 目标

以 GET /api/v1/system/info 为样例打通三端：手写 spec → oapi-codegen 生成 Gin 接口与 TS 客户端 → control-plane 实现 → 前端页面调用展示。

## 验收标准

- [x] spec 含 operationId/summary/description/双 example（§12.2 全要素示范：SystemInfo 成功例 + RFC9457 错误例）
- [x] RFC 9457 错误模型组件定义并可复用（Problem schema + responses 组件 + WriteProblem 出口）
- [x] 浏览器可见接口返回数据 —— **后端腿**：真实二进制 curl 验证通过；前端腿依赖 Vben（M0-007），届时补验并转 done
- [x] 此 spec 作为后续所有端点的模板范例（api/openapi/README 待 M0-005 补充贡献说明）
- [x] gen 一致性门禁首跑通过（gen-openapi.sh --check = OK）

## 涉及模块

- api/openapi/openapi.yaml、api/oapi-codegen.yaml、api/gen/go/
- internal/module/platform/{handler,handler_test}.go
- cmd/control-plane/main.go、go.mod

## DoD 自检

- [x] 核心逻辑单测 3 例：正常返回 / DB 探针降级 notReady / Problem 格式
- [x] lint：go build + go vet 绿（golangci-lint 未装，M0-005 CI 补）
- [x] 文档同步：LICENSES.md 新增 gin/oapi-codegen/runtime 三行
- [x] spec 先行 ✓（本卡即仓库首个 spec 端点）
- [x] commit 带 `[M0-004]`

## 实施记录

### 2026-08-25 · 会话1（后端腿）

- **环境**：用户态安装 go1.27.0（~/.local/go，PATH 入 .bashrc）；GOPROXY 切 goproxy.cn（proxy.golang.org 不可达）；安装 oapi-codegen v2.8.0。
- **做了**：首个 spec（system/info，含 §12.2 全要素）；oapi-codegen 配置（models+gin-server+embedded-spec）；platform.Handler 实现 GetSystemInfo 与 WriteProblem；main.go 以 GinServerOptions.BaseURL 注册 /api/v1。
- **改动文件**：api/openapi/openapi.yaml、api/oapi-codegen.yaml、api/gen/go/api.gen.go（生成物）、internal/module/platform/×2、cmd/control-plane/main.go、go.mod/go.sum、LICENSES.md。
- **验证结果**：BUILD-OK / VET-OK / test ok(3)；gen-check OK；二进制冒烟 `curl :8443/api/v1/system/info` 返回正确 JSON。
- **踩坑留痕**：① oapi-codegen 配置键为 `generate:` 非 `generation:`；② v2.8 无 RegisterHandlersWithBaseURL，前缀经 GinServerOptions.BaseURL 传入——已作为模板范例沉淀于本卡。
- **遗留**：TS 客户端生成管道与页面调用 → 并入 M0-007/M0-008 补验后本卡转 done；ready 字段 PG 探针接线在 M0-006。

### 2026-08-26 · 批量验收

- 用户确认通过，review→done。
