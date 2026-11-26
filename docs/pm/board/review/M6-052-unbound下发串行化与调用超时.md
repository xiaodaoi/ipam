# [M6-052] unbound 下发串行化与调用超时

| 字段 | 内容 |
|---|---|
| ID | M6-052 |
| 状态 | review |
| 来源 | 2026-11-26 全量稳定性审计（批次化整改） |
| 负责 | opencode |
| 创建 | 2026-11-26 |
| 更新 | 2026-11-26 |

## 目标
- `confApplier.apply` 加互斥锁，临时文件改唯一名 + rename，消除并发写坏 conf/zonefile。
- `unbound-control` 全部改 `exec.CommandContext` + 10s 默认超时 + `WaitDelay`，避免控制 socket 卡死导致 `Compile` 永久持锁。
- coherence 对账器：单次调用超时、按 name 去重删除、删除失败不阻断新增、每 5 分钟全量重放以自愈 unbound 重启。

## 改动范围
- `cmd/control-plane/conf_apply.go`、`internal/engine/unbound/{forward,authzone}.go`、`internal/module/coherence/unbound_ctl.go`、`internal/module/dns/intel.go`
- 测试：`conf_apply_test.go`（并发写不混合）、`forward_test.go`/`unbound_test.go`（超时截断、去重、重放）

## 验证
`go test ./...`、`golangci-lint` 0 issues；超时用例验证 150ms 预算内返回。

## 遗留
真实 unbound 卡死/重启的 E2E 需在 staging 注入验证。
