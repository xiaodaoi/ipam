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

## 幂等与限流（M6-021）

- 写请求（POST/PUT/PATCH/DELETE）可携带 `Idempotency-Key: <8-128 位 [A-Za-z0-9._:-]>`：
  - 同 key + 相同方法与载荷，在 TTL（`IPAM_IDEMPOTENCY_TTL`，默认 24h）内**重放首次响应**，
    响应头 `Idempotent-Replay: true`，不会重复执行副作用；
  - 同 key 复用于不同载荷 → `422 IDEMPOTENCY_KEY_REUSED`；
  - 相同 key 的并发请求 → `409 IDEMPOTENCY_IN_PROGRESS`（首个请求完成后可重放）；
  - 未带该头时行为与之前一致；5xx 不缓存，可安全重试。
- 写请求独立限流：默认 **120 次/分钟/IP**（`IPAM_WRITE_RATE_LIMIT` 调整，0 关闭），
  超限返回 `429 WRITE_RATE_LIMITED` 并带 `Retry-After`；读请求仍受全局 `IPAM_RATE_LIMIT`（默认 600/分钟）。

```bash
KEY=$(uuidgen)
curl -s -X POST https://host:8443/api/v1/subnets \
  -H "Authorization: Bearer $TOKEN" -H "Idempotency-Key: $KEY" \
  -H 'Content-Type: application/json' -d @subnet.json
# 网络超时后重试：沿用同一 KEY，得到首次结果而非重复创建
```

完整路径/字段以 `/openapi.json` 为准；`api/openapi/` 为源文件，禁止手改生成物。
