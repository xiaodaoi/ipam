# [M6-017] API/Web/服务网络安全加固

| 字段 | 内容 |
|---|---|
| ID | M6-017 |
| 状态 | review |
| 里程碑 | M6 商业化就绪 |
| 负责 | backend + devops |
| 创建 | 2026-11-01 |
| 更新 | 2026-11-01 |

## 目标

对控制面 API、Web 应用与配套服务做一轮系统性的网络暴露面收敛与协议加固，关闭可被外部利用的常见弱点，并保持可配置、可回退。

约束：**不引入 GPL 依赖**；不破坏 SSE（`/logs/tail`）长连接；单机部署口径不夸大。

## 范围与验收标准

### 1. 反向代理 / 客户端 IP 可信边界
- [x] 显式 `SetTrustedProxies`（`IPAM_TRUSTED_PROXIES`，逗号分隔 CIDR/地址；默认**不信任任何代理**）；
- [x] 未配可信代理时 `ClientIP()` 取自直连 `RemoteAddr`，`X-Forwarded-For` 不可伪造，避免**限流绕过**与审计 IP 污染；
- [x] 单测覆盖：伪造 XFF 不影响 ClientIP（引擎级默认 nil 可信代理）。

### 2. 请求体大小限制（防内存 DoS）
- [x] `/api/v1/*` 统一 `http.MaxBytesReader`（`IPAM_MAX_BODY_BYTES`，默认 1 MiB；0=关闭）；
- [x] 超限返回 413（RFC 9457），不影响审计/限流中间件读取（8KB 上限）；
- [x] 单测覆盖：超限 413、正常放行、非 API 路径不受限。

### 3. HTTP 方法收敛
- [x] 拒绝 `TRACE`/`TRACK`（405）；
- [x] 单测覆盖。

### 4. 安全响应头补全
- [x] 新增 `Cross-Origin-Resource-Policy: same-origin`、`X-Permitted-Cross-Domain-Policies: none`、`X-XSS-Protection: 0`、`X-DNS-Prefetch-Control: off`；
- [x] 提供**默认 CSP**（`IPAM_SECURITY_CSP` 可覆盖；`IPAM_SECURITY_CSP_OFF=1`=关闭），内联脚本用 sha256 放行、不使用 `'unsafe-inline'`，并与 Vben 生产包兼容（浏览器 E2E 14/14 通过）；
- [x] HSTS：TLS 或可信代理 `X-Forwarded-Proto=https`（`IPAM_FORCE_HSTS=1`）时下发（含 `includeSubDomains`）；
- [x] `/metrics`、`/openapi.json` 等非 HTML 响应同样带安全头；
- [x] 单测覆盖新增头与 CSP 默认值。

### 5. `/metrics` 暴露面收敛
- [x] 支持 `IPAM_METRICS_TOKEN`：设置后 `/metrics` 需 `Authorization: Bearer <token>`，否则 401；
- [x] 未设置时保持开放（内网/Compose 内部网络）但启动日志提示风险；
- [x] 单测覆盖带/不带 token；真机 401/200 验证。

### 6. HTTP 服务器与 TLS 加固
- [x] `MaxHeaderBytes` 显式限制（64 KiB）；
- [x] 可配 `ReadTimeout`（默认 60s）；`WriteTimeout` 默认不设以兼容 SSE（`IPAM_HTTP_WRITE_TIMEOUT` 可配）；
- [x] TLS：`MinVersion` 默认 TLS1.2（`IPAM_TLS_MIN_VERSION` 可提升到 1.3），TLS1.2 采用安全套件白名单，禁用重协商；`openssl s_client` 验证；
- [x] 启动日志打印生效的协议/超时/代理配置摘要。

### 7. 部署与文档
- [x] `.env.example` 与 compose 补充 `IPAM_TRUSTED_PROXIES`/`IPAM_MAX_BODY_BYTES`/`IPAM_METRICS_TOKEN`/超时/TLS 版本；
- [x] `install.sh` 生成随机 `IPAM_METRICS_TOKEN` 并写入 Prometheus 抓取配置；
- [x] 运维手册新增「网络安全加固」章节（含 Nginx/反代 `X-Forwarded-For` 配置建议、CSP 调整方法、扫描自检命令）。

## DoD

- [x] 单测/集成测试覆盖上述条目；`go test ./...` 绿
- [x] `golangci-lint` 绿；`gen-check` 不受影响
- [x] 真机验证：安全头、413、TRACE 405、metrics 401、TLS 套件
- [x] 文档与部署说明同步
- [x] commit 带 `[M6-017]`

## 设计取舍

- CSP 默认需兼顾 Vben 生产包（内联样式、hash 资源）；若默认策略破坏页面则回退为仅在显式配置时注入，并记录。
- `WriteTimeout` 与 SSE 冲突：默认关闭，慢客户端写保护交由反代/连接数限制；单机口径明确写「非高可用」。
- 可信代理默认关闭（fail-closed）：容器内若需反代，须显式声明 `IPAM_TRUSTED_PROXIES`，否则限流按直连 IP 计（可能把反代本身当作单一客户端，属可接受的安全侧默认）。

## 涉及文件

- `internal/module/platform/security.go`、`security_test.go`（新增 body limit / methods / metrics auth）
- `cmd/control-plane/main.go`（trusted proxies、body limit、server 超时、TLS、metrics 保护接线）
- `deploy/compose/{compose.yaml,.env.example,install.sh}`、`observability/prometheus.yml`
- `docs/manual/operations.md`、`docs/dev/M6-017-网络安全加固实现说明.md`
- 本卡、`docs/pm/progress-log.md`、计划文档

## 实施记录

### 2026-11-01 · 会话1

- **做了什么**：
  - **可信代理**：`SetTrustedProxies(IPAM_TRUSTED_PROXIES)`，默认不信任任何代理（`ClientIP` 取直连，防 XFF 伪造绕过限流/污染审计）；
  - **请求体上限**：`platform.BodyLimit` 限 `/api/v1/*`（默认 1 MiB），超限 413；`RejectTrace` 拒 `TRACE`/`TRACK` 405；
  - **安全头补全**：CORP/X-Permitted-Cross-Domain-Policies/X-DNS-Prefetch-Control/X-XSS-Protection:0；默认 CSP 对内联引导脚本按 sha256 放行（不用 unsafe-inline），兼容 iconify CDN 与 Vben；
  - **/metrics**：`IPAM_METRICS_TOKEN` 可选 Bearer 保护；
  - **服务器/TLS**：`MaxHeaderBytes=64KiB`、可配 `ReadTimeout`（默认 60s）、`WriteTimeout` 默认不设（兼容 SSE）、TLS `MinVersion` 可配 + TLS1.2 AEAD 套件白名单 + 曲线偏好；
  - **部署/文档**：compose/.env.example/install.sh/prometheus/observability README/运维手册。
- **改动文件**：`internal/module/platform/security.go`（+`security_test.go`）、`cmd/control-plane/main.go`（+`main_test.go`）、`deploy/compose/{compose.yaml,.env.example,install.sh}`、`observability/{prometheus.yml,compose.observability.yaml,README.md}`、`.gitignore`、`docs/manual/operations.md`、`docs/dev/M6-017-网络安全加固实现说明.md`。
- **验证结果**：单测全绿、lint 0 issue、gen-check 一致、两套 compose config 通过；真机安全头齐全、CSP 哈希与 index.html 内联脚本一致、TRACE 405、2MB 体 413；Playwright E2E 14/14；临时 TLS 端口 tls1_1 拒绝 / tls1_2 与 tls1_3 通过 / HSTS 下发；metrics 无/错令牌 401、正确 200。
- **遗留**：CSP 仍允许 iconify 公网 CDN（内网化需离线打包图标）；限流多实例共享随 M6-003B；镜像 CVE 扫描需网络未入门禁。
