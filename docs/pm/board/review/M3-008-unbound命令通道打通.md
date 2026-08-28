# [M3-008] unbound 命令通道打通（sock 模式注入）

| 字段 | 内容 |
|---|---|
| ID | M3-008 |
| 状态 | review |
| 来源 | M3-001/M3-005 遗留（X-Unbound-Warning: UNBOUND_DOWN）；与 M3-007 对称的 DNS 域真实下发缺口 |
| 负责 | opencode(backend/devops) |
| 创建 | 2026-08-28 |
| 更新 | 2026-08-28 |

## 目标

control-plane 的 unbound-control 客户端读到正确配置（unix sock 模式），打通上游转发/缓存清空/解析记录/解析测试台等全部 DNS 运行时命令的真实下发。

## 根因（已诊断）

- unbound 容器：`remote-control` 为 sock 模式（/run/ipam/unbound-ctl.sock，无证书，共享卷可见，ipam 组可读写）
- control-plane 容器内置 /etc/unbound/unbound.conf 是 `control-enable: no`（TCP 8953）→ 客户端读错配置 → Connection refused

## 验收标准（可测）

- [ ] ExecController.Conf 字段（-c 参数注入）+ cmdArgs 单测
- [ ] compose：control-plane 挂载 config/unbound 只读 + IPAM_UNBOUND_CONF 注入；dev.sh 同步
- [ ] 容器 e2e：POST /upstreams → 无 X-Unbound-Warning（forward_add 成功）；unbound 日志确认
- [ ] lint 0、commit [M3-008]

## 遗留边界

- settings 渲染写 confPath（control-plane 容器内副本）与 unbound 容器真实配置不同文件——**conf 持久化路径设计**（共享写卷 + reload）后续卡
- 本卡只打通运行时命令通道（forward_add/remove、flush、auth_zone_reload、reload）

## 实施记录（追加式，勿删旧条目）

### 2026-08-28 · 会话1
- **做了**：ExecController 加 Conf 字段（unbound-control -c 注入客户端配置）+ cmdArgs 辅助（单测锚定）；main 经 IPAM_UNBOUND_CONF 环境变量注入；compose control-plane 挂载 config/unbound:/etc/unbound-ctl:ro；dev.sh 注入宿主路径。
- **根因**：control-plane 内置 /etc/unbound/unbound.conf 是 control-enable:no（TCP 8953），unbound 容器真实配置是 unix sock 模式（/run/ipam/unbound-ctl.sock，共享卷）——客户端读错配置。
- **验证结果**：容器 e2e——POST /upstreams → **201 无 X-Unbound-Warning**（forward_add 经 sock 真实成功，修复前每次 APPLY_FAILED）；全链测试绿 + lint 0。
- **踩坑留痕**：sock 权限排查——head 读 sock 报 "No such device or address" 是对 socket 做 read 的正常表现（open 成功=权限 OK），不是缺失。
- **遗留**：settings 渲染写 confPath 是 control-plane 容器内副本，unbound 容器真实配置不同文件——**conf 持久化路径设计**（渲染产物共享卷 + reload 流程）后续卡；本卡只打通运行时命令通道（forward_add/remove、flush、auth_zone_reload、reload）。
