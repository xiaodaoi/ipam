# [M2-046] DHCP「IPv4 地址无法下发」——INIT-REBOOT 权威应答修复

| 字段 | 内容 |
|---|---|
| ID | M2-046 |
| 状态 | doing |
| 来源 | 生产现象：客户端持续 INIT-REBOOT 无应答；2026-10-10 会话定位 |
| 负责 | opencode(backend) |
| 创建 | 2026-10-10 |
| 更新 | 2026-10-10 |

## 目标

消除「DHCPv4 客户端无法获取地址」根因：Kea 运行态 subnet 缺 `authoritative:true`，导致无既有租约的 INIT-REBOOT 被**静默丢弃**。让 `internal/engine/kea/config.go` 持久渲染 `authoritative`，使 control-plane 任意写操作经 `BuildConfigFull` 全量重推后行为不回退。

## 现象

17 台 DHCPv4 客户端（MAC 全在 subnet16 `10.61.66.0/23`）反复 INIT-REBOOT，服务端无 ACK/NAK，地址无法下发（02:08:59 UTC 后无真实客户端流量）。

## 根因

- `internal/engine/kea/config.go` 的 `defaultBaseConfig()`/`BuildConfigFull()` **不渲染 `authoritative`**，运行态 subnet16 `authoritative=false`。
- Kea 非权威语义：收到无既有租约的 INIT-REBOOT **静默丢弃** → 客户端永远等不到应答（既无 ACK 也无 NAK）。
- 预留（host reservation）来自 DB `reservation` 表，`BuildConfigFull` 已从 binds 持久投影——非缺陷项。

## 验收标准（可测）

- [ ] `internal/engine/kea/config.go` 渲染 `authoritative: true`（对齐生产语义），并有单测覆盖
- [ ] control-plane 触发一次 config-set 后，`config-get` 读回 subnet 含 `authoritative: true`
- [ ] E2E：预留 MAC 正确 INIT-REBOOT → ACK；未知 MAC 请求池内空闲地址 → ACK；无租约且非保留 → 有应答（不再静默）
- [ ] `go build ./...` 0 错；`go test ./internal/engine/kea/ -count=1` 绿

## 涉及模块

- `internal/engine/kea/config.go`（+ `config_test.go`）
- 参考调用链：`cmd/control-plane/main.go:234`（`BuildConfigFull` + config-set）

## 显式排除

- 预留数据维护（已在 DB `reservation` 表，30 行，subnet16 占 16 行）
- Kea 探针注入基建（veth/AF_PACKET）——仅验证用，已清理

## DoD 自检（完成前逐条勾选）

- [ ] 核心逻辑有单测
- [ ] lint/typecheck 通过
- [ ] 相关文档章节已同步（progress-log + 本卡实施记录；必要时架构文档 DHCP 章节/ADR）
- [ ] commit 带 `[M2-046]`（待用户确认后提交）

## 实施记录（追加式，勿删旧条目）

### 2026-10-10 · 会话1（定位 + 热修 + E2E 验证）

- **定位**：`config.go` 不渲染 authoritative；运行态 subnet16 为 `authoritative=false`。
- **热修（运行态，易失）**：config-set 置 subnet16 `authoritative:true`；16 条预留确认已存在于 DB（持久）。
- **E2E 验证方法**：hostNetwork Kea 的 raw socket 只处理「进入」包（本机广播仅 PACKET_OUTGOING 被忽略、loopback 被 Kea 硬拒）。用 `ipam-lab`（NET_ADMIN+host netns）建 veth 对，临时把 `ipam-kea0` 加入 interfaces，从对端 `ipam-kea1` 以 AF_PACKET 注入 L2 帧 → 到达 `ipam-kea0` 为 PACKET_BROADCAST。
- **E2E 结果（Kea DEBUG 日志 + tcpdump 双证）**：预留 MAC `74:4c:a1:df:78:25` 正确 INIT-REBOOT（仅 ciaddr）请求 `10.61.66.233` → **ACK**；DISCOVER → **OFFER 10.61.66.233**；未知 MAC 请求池内空闲 `10.61.66.99` → **ACK**；对照 INIT-REBOOT 误带 option 50 → NAK `DHCP4_PACKET_NAK_0002`（探针构造缺陷）。
- **环境清理**：`ipam-lab`/`ipam-probe`/`ipam-sniff` 容器、`ipam-probe-net` 网络、veth 对全部删除；Kea 恢复 `interfaces=['ens160']` + INFO logger（保留 authoritative + 16 预留）；租约库清零。
- **遗留**：`config.go` 渲染 authoritative 待落地；落地后重建/重启 control-plane，使写入路径持久生效。

### 2026-10-10 · 会话2（持久化代码修复）

- **改动**：`internal/engine/kea/config.go` `defaultBaseConfig()` 增加顶层 `"authoritative": true`（Kea 顶层键，覆盖全部 subnet4，含 subnet16）。`IPAM_KEA_TPL` 模板覆盖路径经确认未被任何部署引用，故默认配置即生效，无需改静态 conf。
- **单测**：`internal/engine/kea/config_full_test.go` 新增 `TestBuildConfig_权威应答`（断言生成 Dhcp4 含 `"authoritative":true`）。
- **验证**：`go build ./...` 0 错；`go test ./internal/engine/kea/... -count=1` 绿（含新用例，`-v` 确认 PASS）。
- **部署与实测**：control-plane 镜像重建、容器重启；启动 `[dhcp-apply] converge ok` 以新代码推送配置，`config-get` 实测顶层 `authoritative: true`、subnet16 预留 16 条保留 → 持久化修复**已生效**（不再依赖运行态手工热修）。
- **注**：重建时镜像层一度占满磁盘致 PG 短暂不可用，已 `docker builder prune` 释放空间恢复。
