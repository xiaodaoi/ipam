# [M2-013] daemon 动态装载绑定模板

| 字段 | 内容 |
|---|---|
| ID | M2-013 |
| 状态 | review |
| 来源 | M2-012 遗留①；§4.3 多池对、§2.1 联动时序 |
| 负责 | opencode(backend) |
| 创建 | 2026-08-28 |
| 更新 | 2026-08-28 |

## 目标

coherence-daemon 从 PG prefix_template 动态装载启用模板（周期刷新），替换 PoC 内置 t-default，使双栈管理页创建的模板实际参与 v4→v6 联动匹配（最长前缀自动选模板）。

## 验收标准（可测）

- [ ] daemon PG 模式启动输出装载条数；PG 抖动/查询失败时保留旧缓存（降级不瘫痪）
- [ ] v6 前缀裸地址与 CIDR 两形态均可投影为 ApplyTemplate 可拼前缀；enabled=false 不装载（单测）
- [ ] Lookup/All 缓存读写并发安全（All 返回拷贝）
- [ ] e2e：双栈 API 新建模板 ≤ 刷新周期内 daemon 可用
- [ ] lint 0、coherence 包测试绿

## 涉及模块

- `cmd/coherence-daemon/main.go`（装配 lookup/tplAll，connectPG 返回池复用）
- `internal/module/coherence/tplloader.go`（新增：缓存式装载器）
- `deploy/compose/compose.yaml`（无需改：daemon 已传 DSN）

## 实施记录（追加式，勿删旧条目）

### 2026-08-28 · 会话1
- **做了**：TplLoader（pgx 查询 enabled 行 → projectTemplate 投影：v6 容忍裸地址/CIDR 两形态、v4 严格 CIDR；RWMutex 缓存；30s 刷新环）；main.go PG 模式接线 lookup=tl.Lookup、tplAll=tl.All（SetTemplateAll 已有通道），PoC 模式保留 t-default；Refresh 失败保留旧缓存。
- **验证**：coherence 新增 4 测试（归一化/Applyable/缓存拷贝/最长前缀）全绿；lint 0。
- **验证**：容器 e2e——daemon 日志 `loaded 1 templates from PG`（双栈页此前建的模板被装载）；Refresh 失败保旧缓存语义有测试。
- **遗留**：prefix_template 变更 NOTIFY 实时推送（当前 30s 周期，可接受）P2。
