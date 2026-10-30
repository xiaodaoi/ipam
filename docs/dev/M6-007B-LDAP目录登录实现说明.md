# M6-007B LDAP/AD 目录登录实现说明

## 1. 数据模型

`db/postgresql/migrations/0035_ldap.sql`：

- `users.auth_source`（`local|ldap`，默认 `local`）：区分本地账号与目录影子账号；
- `ldap_settings` 单行表：服务器地址、StartTLS、跳过校验、Bind DN/口令、Base DN、用户过滤器、组搜索 Base DN/过滤器/属性、`role_map`（jsonb）、默认角色。

`role_map` 示例：

```json
{"ipam-ops": "operator", "cn=ipam-admins,ou=groups,dc=corp,dc=local": "admin"}
```

## 2. 认证流程

`internal/module/platform/ldap.go`：

1. 读取设置，未启用直接返回 `LDAP_DISABLED`；
2. `dialLDAP`：按 `ldap://`/`ldaps://` 建连，可附加 StartTLS；
3. 服务账号 bind（口令可被 `IPAM_LDAP_BIND_PASSWORD` 覆盖，避免落库）；
4. 用 `user_filter`（`{username}` 替换并转义）搜索用户 DN；
5. 可选：按 `group_filter`（`{dn}`）搜索组，读取 `group_attr` 与 `memberOf`；
6. 用用户 DN + 用户口令重新 bind 校验口令（空口令在入口拦截，防匿名 bind）；
7. `mapRoles` 将组映射为角色（支持完整 DN、`cn=xxx` 或纯组名，逗号/分号分隔多角色）。

`AuthHandler.AuthLogin`：

- 本地账号（`authSource=local`）仍走 bcrypt，目录不可达不影响；
- `authSource=ldap` 或本地不存在的账号 → 目录认证；
- 认证成功由 `syncLDAPUser` 落地影子账号：首次创建（随机本地口令），显示名/角色变化时更新，角色变化 `token_version+1` 吊销旧会话；
- 目录不可达 → 503 `LDAP_UNAVAILABLE`；未启用 → 401（不暴露后端状态）。

## 3. API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/system/ldap` | 读取设置，不返回 `bindPassword`，用 `hasBindPassword` 表示 |
| PUT | `/system/ldap` | 保存；`bindPassword` 留空保持原值 |
| POST | `/system/ldap/test` | 用指定账号真实 bind，返回显示名/组/映射角色，不创建账号 |

三者均要求 `system:read|write`。

## 4. RBAC 修复

`domainOf` 增加 `/api/v1/system/` → `system` 域，写方法判定加入 `PUT`。
此前 `/system/security-settings`、`/system/log-settings`、`/system/backups` 等未归域，
任意已认证用户可修改系统配置；本次一并修复并新增回归测试。

## 5. 前端

- 路由：`/system/directory`（系统管理 → 目录服务）；
- 页面：连接配置、组映射表（增删）、连通性测试与结果展示；
- 账号页：`authSource=ldap` 显示「目录账号」标签，并禁用「重置口令」。

## 6. 验证

- 真实 OpenLDAP（`osixia/openldap:1.5.0`）：
  - 保存设置成功、`hasBindPassword=true`、口令不回显；
  - 连通性测试返回 `roles=["operator"]`（组 `ipam-ops` 命中）；
  - 目录账号登录成功并创建 `authSource=ldap` 影子账号；
  - 错误口令 401；目录账号本地改密 400 `DIRECTORY_ACCOUNT`。
- 单测：`mapRoles` 匹配规则、设置保存/口令掩码、空口令拒绝、影子账号同步与吊销、目录故障降级、RBAC 系统域。
- `vue-tsc`、eslint、`go test ./...`、`golangci-lint`、E2E 通过。

## 7. 运维要点

- 生产优先 `ldaps://` 或 StartTLS；「跳过证书校验」仅测试用；
- 服务账号口令建议用 `IPAM_LDAP_BIND_PASSWORD` 注入，不落库；
- 组映射变更在用户下次登录生效；
- 目录故障时目录账号无法登录（503），本地管理员账号仍可登录处置。
