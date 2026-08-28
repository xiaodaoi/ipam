# [M5-005] 迁移 runner（增量迁移自动应用）

| 字段 | 内容 |
|---|---|
| ID | M5-005 |
| 状态 | review |
| 来源 | M5-004 遗留 P1（踩坑：initdb.d 仅首卷生效，0010 需手动 psql） |
| 负责 | opencode(backend/devops) |
| 创建 | 2026-08-28 |
| 更新 | 2026-08-28 |

## 目标

控制面启动时自动应用未执行的迁移（schema_migrations 记账 + 存量库基线），后续新增迁移（0011+）零手工介入。

## 验收标准（可测）

- [ ] schema_migrations 表记账；启动按文件序应用未执行迁移（简单协议多语句）
- [ ] 基线语义：存量库（探测 0009 产物表）≤0009 记为已应用不重放；全新库从 0001 全量执行
- [ ] 单测：文件列举排序/待执行计算（fstest.MapFS）；lint 0
- [ ] e2e：drop users + 清记账 → 重启 → 自动重建+播种 → /users 201 + admin 登录 ✓
- [ ] compose 挂载 migrations 只读卷 + dev.sh 注入路径；未配置目录时跳过（纯内存模式）
- [ ] commit [M5-005]

## 涉及模块

- `internal/pkg/migrator/migrator.go`（新增）
- `cmd/control-plane/main.go`（pool 建立后执行，失败 fatal）
- `deploy/compose/compose.yaml`（control-plane 只读挂载 + env）
- `scripts/dev.sh`（export IPAM_MIGRATIONS_DIR）

## 实施记录（追加式，勿删旧条目）

### 2026-08-28 · 会话1
- **做了**：internal/pkg/migrator（schema_migrations 记账、按文件序简单协议多语句执行、存量库基线：探测 0009 产物表 operation_audit → ≤0009 记账不重放）；main.go pool 建立后执行（失败 fatal 快速暴露）；compose 只读挂载 /srv/migrations + dev.sh 注入宿主路径；未配置目录跳过（纯内存 PoC 模式）。
- **验证结果**：单测 3 组全绿（列举排序/versionPrefix 前缀比较/待执行计算，fstest.MapFS）；容器 e2e 三段——①首启基线 9 项记账 + 0010 幂等应用；②增量场景 drop users+清记账 → 重启自动重建+播种 → admin 登录 ✓ / POST /users 201 ✓；③修复后重启"无待应用迁移（10 个文件均已记账）" + 登录 200。
- **踩坑留痕**：字典序陷阱两处——"0009_operation_audit" > "0009"，基线跳过（computePending）与标记循环都必须用 versionPrefix 数字前缀比较（TestVersionPrefix 回归锚点）；首启日志"9 项"是 len(files)-len(pending) 差值，实际标记循环漏了 0009，重启重放才暴露——两处同根因必须一起修。
- **遗留**：非幂等迁移文件在部分初始化卷上会失败即 fatal（快速暴露，可接受）；baselineThrough=0009 为一次性常量，后续迁移全走 runner。
