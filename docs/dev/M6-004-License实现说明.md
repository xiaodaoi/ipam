# M6-004 License 实现说明

## 1. 文件格式

```json
{
  "payload": "base64url(JSON)",
  "signature": "base64url(ed25519(payload))"
}
```

payload：

```json
{
  "licenseId": "LIC-...",
  "issuedTo": "ACME",
  "edition": "enterprise",
  "features": ["ha", "ldap"],
  "maxTerminals": 5000,
  "issuedAt": "2026-10-30T00:00:00Z",
  "expiresAt": "2027-10-30T00:00:00Z",
  "graceDays": 0
}
```

## 2. 签发

```bash
# 生成密钥对（只做一次；私钥交授权方，公钥配到客户环境）
go run ./cmd/licensegen -gen-key

# 签发
IPAM_LICENSE_PRIVATE_KEY=<base64-private> go run ./cmd/licensegen \
  -id LIC-2026-001 -to "ACME" -edition enterprise \
  -features ha,ldap,siem -max 5000 -days 365 -out license.json
```

客户环境：

```env
IPAM_LICENSE_PUBKEY=<base64-public>
IPAM_LICENSE_FILE=/srv/log-exports/license.json
```

## 3. 校验

- 公钥解析：base64 32 字节或 PEM PKIX；
- 签名：Ed25519.Verify(payload, signature)；
- edition 必须是 lite/standard/enterprise；
- 过期时间 + graceDays；
- 未配置公钥或文件不存在：community，不阻断现有功能；
- 激活失败不覆盖旧文件（先校验再原子 rename）。

## 4. 页面

`/system/license`：

- KPI：当前版本 / 授权状态 / 到期时间 / 终端上限；
- 授权信息：客户、License ID、能力标签、文件路径、状态说明；
- 激活：粘贴 JSON → 服务端验签；移除：二次确认回社区版。

## 5. 当前边界

- 不做硬停机：过期/未授权时仅状态与能力位变化；商业功能拦截在后续按 `features` 接入；
- 终端上限未计量：需要从 Kea 租约 distinct MAC/DUID 统计后做软降级；
- 无在线激活/试用 License：当前为离线文件模式。
