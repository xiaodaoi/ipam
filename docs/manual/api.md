# API 使用

- 规范：OpenAPI 3.1，在线地址 `https://<host>:8443/openapi.json`；
- 认证：`Authorization: Bearer <accessToken>`；
- 登录：`POST /api/v1/auth/login`（可选 `totpCode`）；
- 刷新：`POST /api/v1/auth/refresh`（refreshToken）；
- 错误：RFC 9457 `application/problem+json`，含 `code/detail`；
- 分页：游标式（`cursor`/`nextCursor`）或 `pageSize` 由端点定义；
- 权限：JWT 角色 → 6 域读写；403 表示 scope 不足。

示例：

```bash
TOKEN=$(curl -s -X POST https://host:8443/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"..."}' | jq -r .accessToken)

curl -s https://host:8443/api/v1/subnets -H "Authorization: Bearer $TOKEN"
```

危险操作：

- 恢复备份：`POST /system/backups/{id}/restore` 必须 `{"confirm":true}`；
- 重启控制面：`POST /system/restart`；
- 删除子网/角色/用户等有二次确认与保护（最后管理员、引用检查）。

完整路径/字段以 `/openapi.json` 为准；`api/openapi/` 为源文件，禁止手改生成物。
