# [M5-003] RBAC 写权限拦截中间件

| 字段 | 内容 |
|---|---|
| ID | M5-003 |
| 状态 | review |
| 来源 | §12.3/§13.2 权限码同源；M5-002 遗留（scope 只记录不拦截） |
| 负责 | opencode(backend) |
| 创建 | 2026-08-28 |
| 更新 | 2026-08-28 |

## 目标
变更类请求（POST/PATCH/DELETE）按 JWT roles 强制拦截：user 角色写操作 403，admin 放行；读端点与认证/探活白名单免鉴权。与审计中间件共享 JWT 解析。

## 验收标准
- [ ] user 角色令牌 POST/PATCH/DELETE 一律 403 FORBIDDEN
- [ ] admin 令牌写操作放行；无令牌访问受保护端点 401
- [ ] 白名单（/auth/*、/system/info、GET 全部）不拦截

## 涉及模块
- internal/module/platform/auth_jwt.go（claims/角色）
- internal/module/platform/rbac.go（新增中间件）
- cmd/control-plane/main.go（装配于审计中间件之前）

## DoD 自检
- [ ] 核心逻辑有单测
- [ ] lint 通过
- [ ] 文档同步（§12.3 落地说明）
- [ ] API 契约零变更（拦截行为属 401/403 语义补全）
- [ ] commit 带 [M5-003]

## 实施记录（追加式，勿删旧条目）

### 2026-08-28 · 会话1
- **做了**：NewRBACMiddleware（POST/PATCH/DELETE 须 admin JWT；无令牌 401 TOKEN_MISSING、user 角色 403 FORBIDDEN；login 端点与 GET/HEAD 白名单放行）；装配于审计中间件之前（403 请求不入审计）。
- **验证结果**：5 组单测（admin 放行/user 403+FORBIDDEN body/无令牌 401/login 绕行/bot-admin 放行）+ 容器实测 admin POST 201、无令牌 401、读端点不受限；golangci 0；全仓 go test 绿。
- **设计取舍**：签发仅暴露 admin 角色（登录口令单账号），外部无法构造 user 令牌——403 分支当前由单测保证，多用户落地（P1）时自然激活。
- **遗留**：端点级细粒度 scope（§13.2 权限码逐端点映射）P2；多用户与 Bot Token 管理界面 P1。
