# [M6-004] License 与版本分级

| 字段 | 内容 |
|---|---|
| ID | M6-004 |
| 状态 | review |
| 里程碑 | M6 商业化就绪 |
| 负责 | backend + frontend + devops |
| 创建 | 2026-10-29 |
| 更新 | 2026-10-30 |

## 目标

建立离线授权的最小商业闭环：

- Ed25519 签名 License 文件（payload + signature）；
- 版本/客户/能力/终端上限/到期时间；
- 服务端校验（签名、edition、有效期、宽限期）；
- 管理页展示与激活/移除；
- 授权签发 CLI（私钥由授权方保管，不入库）；
- 未配置/未激活时回退社区版，不影响现有功能。

## 验收标准

- [x] `IPAM_LICENSE_PUBKEY` 支持 base64 32 字节或 PEM；
- [x] `IPAM_LICENSE_FILE` 默认路径可配置（compose：`/srv/log-exports/license.json`）；
- [x] `GET /system/license` 返回 licensed/edition/features/maxTerminals/expiresAt/daysRemaining/message；
- [x] `PUT /system/license` 验签+有效期后原子写入，失败不覆盖现有授权；
- [x] `DELETE /system/license` 回到 community；
- [x] 未配置公钥时进入 community 并给出明确提示；
- [x] 前端 `/system/license`：4 KPI、授权信息、能力标签、激活/移除；
- [x] `cmd/licensegen -gen-key` 生成密钥对；`-id/-to/-edition/-features/-max/-days` 签发；
- [x] 单测覆盖有效/篡改/过期/无公钥/激活/移除；
- [x] `go test`、`vue-tsc`/eslint/build、gen-check 通过。

## 设计说明

- 授权文件不加密，仅签名；客户可读不可改；
- 到期后状态为未授权，但当前版本不做硬停机（软降级）；
- `maxTerminals` 仅展示，终端计量与超限软降级在后续版本接入；
- `features` 为后续 HA/LDAP/SIEM/OEM 的灰度能力位。

## 涉及文件

- `api/openapi/{paths/license.yaml,components/schemas/license.yaml}`、`api/gen/**`
- `internal/module/platform/license.go`、`license_test.go`
- `cmd/licensegen/main.go`
- `cmd/control-plane/main.go`
- `web/apps/web-ipam/src/views/system/license/index.vue`、`api/ipam.ts`、路由/文案
- `deploy/compose/{compose.yaml,.env.example}`
- 文档：本卡、实现说明、进度日志

## 实施记录

### 2026-10-30

- 后端：Ed25519 验签、edition/过期/宽限校验、原子写文件；未激活/无公钥回退 community。
- 签发工具：`cmd/licensegen` 支持生成密钥、签发离线 JSON。
- 前端：License 页（版本/状态/到期/终端/能力/激活/移除）。
- 部署：compose 注入 `IPAM_LICENSE_FILE`/`IPAM_LICENSE_PUBKEY`。
- 验证：单测 5 类场景通过；前后端构建/lint 通过。
- 遗留：终端计量/超限软降级、能力位强制拦截、在线激活与试用 License（后续）。
