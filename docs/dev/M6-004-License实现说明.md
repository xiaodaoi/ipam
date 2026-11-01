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


## 终端计量与超限软降级（M6-004B，2026-11-01）

### 数据口径

- **活跃终端** = 近 24 小时内出现在 DHCP 日志中的去重客户端 MAC（`logquery.MacActivity`）；
- 计数源以 `LicenseService.SetTerminalCounter` 注入，`platform.TerminalCounter` 提供 60s TTL 缓存：
  - `/system/license` 与 `/metrics` 共用同一份缓存，避免每次抓取扫描日志表；
  - 计数源失败时返回上次成功值（降级），从未成功过才把错误暴露到 message。

### 超限语义

- 仅当「已授权且 `maxTerminals > 0`」才判定超限；`0` 表示不限，社区版不上报超限；
- 超限**不拦截任何请求**：message 追加「活跃终端 N 台已超授权上限 M 台（仅告警，不中断服务）」，`terminalsOverLimit=true`；
- 未超限时 message 追加「活跃终端 N/M 台」，便于审计留痕。

### 观测

```
ipam_license_terminals_used    # 近 24h 去重 MAC
ipam_license_terminals_limit   # 0=不限
```

`IpamLicenseTerminalsOverLimit`：`used > limit and limit > 0` 持续 6h，severity=warning。

### 前端

- KPI「终端计量」：`used / 上限 台`，超限切换 amber 并把 hint 改为超出数量；
- 授权信息卡片新增「活跃终端（近 24h）」字段与「终端超限」标签；
- 说明文案统一为「超限仅告警不中断」。

### 边界

- 终端数依赖日志采集链路（Vector → ClickHouse）；采集停滞时计数会滞后，但不影响控制面可用性；
- 按 MAC 去重：同一设备换网卡/随机化 MAC 会重复计数；如需资产口径，可在后续接入台账/资产表。
