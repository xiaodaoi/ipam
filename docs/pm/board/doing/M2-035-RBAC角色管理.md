# [M2-035] RBAC 角色管理（管理员/操作员/审计员/自定义 + 菜单级授权 + 接口拦截）

| 字段 | 内容 |
|---|---|
| ID | M2-035 |
| 状态 | doing |
| 来源 | 用户前端调试清单（五-3/5-4）：角色分为管理员/操作员/审计员/自定义；角色管理可自定义角色、按菜单授权（查看/编辑）；**不只是隐藏菜单——接口同样拦截** |
| 负责 | opencode(backend+frontend) |
| 创建 | 2026-08-31 |
| 更新 | 2026-08-31 |

## 方案（权限模型：6 域 × 读写 = 12 权限点）

1. **域**：dash/logs/dhcp/dns/system/assets；**权限点**：`<域>:read` / `<域>:write`
2. **内置角色**（硬编码映射）：admin（全量）/operator（全读+dhcp/dns/assets 写+system:read）/auditor（全读+system:read）/user（只读 5 点）
3. **自定义角色**：roles 表（迁移 0016：name/permissions JSON/builtin）——批 1 已建表+种子
4. **接口拦截**：RBAC 中间件重构——`domainOf(FullPath)` 路径→域、方法→read/write、`hasPerm` 逐角色解析（内置映射优先 + permLookup 查 roles 表，main 装配闭包）
5. **批 2**：roles API（spec/gen/后端/前端角色管理页——权限矩阵 checkbox）
6. **批 3**：前端菜单过滤（用户权限→菜单显隐）+ e2e + 收尾

## 验收标准（可测）

- [x] 批 1：迁移 0016 四内置角色种子实收；**admin 全通**（dash/subnets/users 200）；**operator 权限集生效**（system:read 200/dns:write 201）
- [ ] 批 1 补：**域权限 403 决定性验证**（op POST /users → 403 需要 system:write）
- [ ] 批 2：roles API + 前端角色管理页
- [ ] 批 3：前端菜单过滤 + e2e + 收尾

## 实施记录（追加式，勿删旧条目）

### 2026-08-31 · 会话1（批 1）
- **做了**：迁移 0016（roles 表+四内置种子）；rbac.go 重构（domainOf/builtinRolePerms/hasPerm——内置映射优先+permLookup 查库；NewRBACMiddleware 加 permLookup 参数；域权限拦截替换 admin 硬编码）；main.go permLookup 闭包（roles 表查询）。
- **踩坑留痕**：① `NewRBACMiddleware` 子串替换把 permLookup 插进了 `r.Use(platform.` 与调用之间（r.Use(platform. 前缀在 m.group(0) 外）——**子串替换必须看前缀上下文**；② permLookup 用 json.Unmarshal 需 main 补 encoding/json import；③ rbac_test 调用形态是 `(store, bl)` 局部变量（非 NewMemUserStore() 形态）——**锚点按实际调用点**。
- **验证结果**：迁移 0016 四内置种子实收；admin 全通（dash/subnets/users 200）；operator 权限集生效（system:read 200/dns:write 201）——权限模型工作正常。
- **遗留**：域权限 403 决定性验证（本批补）；批 2/3。

### 2026-08-31 · 会话2（批 2a：roles API 后端闭环）
- **做了**：spec（/roles CRUD 四操作 + Role/RoleList/RoleCreate/RoleUpdate schema）+ gen；role_store.go（RoleStore 接口/Mem/Pg 两实现 + RegisterRole 联动 normalizeRoles 白名单）；roles_handler.go（四方法 + builtin 409 保护 + validatePerms 权限点校验 + gen 枚举类型 string 转换）；main.go 装配（roleStore/rolesH/full 结构字段+值/启动加载自定义角色注册）。
- **验证结果**：e2e 决定性——GET /roles 内置 4 角色实收（admin 12 权限/operator 9/auditor 6/user 5）；POST /roles 自定义 dhcp-viewer 201；用户绑定自定义角色后 **GET /subnets 200（dhcp:read 生效）/ GET /users 403（system:read 缺——决定性）**；DELETE /roles/admin → 409 BUILTIN_ROLE 保护；全链绿（build/test/lint/typecheck）。
- **踩坑留痕**：① gen 的 permissions 字段带 enum 时生成独立类型（[]apigen.RoleCreatePermissions）——需 string 转换循环；② Role schema 无 enum 时生成 []string（toGenRole 直接透传）；③ roleId 主键是 name（string）——spec 不能写 format: uuid（gen 生成 rtypes.UUID 签名不匹配）；④ handler_test 在 platform 包内——包内引用不需要 platform. 前缀。
- **遗留**：批 2b（前端角色管理页）；批 3（前端菜单过滤）。
