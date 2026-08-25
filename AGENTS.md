# AGENTS.md — IPAM 项目协作纪律（人 + AI 代理通用）

> 单一事实源：`docs/架构设计文档-V1.md`。实现与文档冲突时以架构文档为准；发现冲突先提任务卡修订文档，再动代码。

## 项目速览

IPAM 一体化产品：IPAM + DHCPv4/v6(Kea) + DNS(Unbound) + 统一日志(ClickHouse)，Go+Gin 控制面，Vben Admin v5 前端。目录职责见架构文档 §14，主导航与权限码见 §13.4。

## 常用命令

> M0-003 落地后生效；当前阶段按任务卡内注明的临时命令执行。

```bash
make build        # 构建全部
make test         # go test + web 单测
make lint         # golangci-lint + eslint/oxlint
make gen          # 从 api/openapi 重新生成代码（api/gen 唯一再生途径）
```

## 禁改区

- `web/packages/@vben/**` —— 上游底座，业务只写 `web/apps/web-ipam/`
- `api/gen/**` —— 仅允许 `make gen` 重新生成，禁止手改
- `deploy/images/` 中 unbound 版本 ≥1.16 约束（风险 K8）变更须连带 CI 断言复核

## 安全红线

- 密钥/证书/口令不入库（`.env` 已忽略，运行时经 `/run/secrets`）
- 不引入 GPL 依赖（许可矩阵维护于 `LICENSES.md`）

## 强项目管理纪律（每次会话必须遵守）

1. **一会话一任务卡**：开始时读 `docs/pm/board/<列>/Mx-nnn-*.md`；结束时更新卡片状态并移动到对应列。
2. **DoD 五条**（完成前逐条自检）：核心逻辑有单测 / lint+typecheck 绿 / 相关文档章节已同步 / API 变更先改 spec 再生成 / commit 带 `[Mx-nnn]`。
3. **进度记录三件套**（会话收尾必做）：
   a. 任务卡「实施记录」追加一段（做了什么 / 改动文件 / 验证结果）；
   b. 在 `docs/pm/progress-log.md` 顶部追加一条带日期的进度条目；
   c. 涉及新决策 → 先更新架构文档 ADR/章节，再继续编码。
4. **实现说明文档**：里程碑出口或跨模块功能完成时，在 `docs/dev/` 撰写实现说明（设计对接点、关键代码路径、验证证据）。
5. **commit 规范**：`<type>(<scope>): [Mx-nnn] 描述`，type ∈ feat/fix/docs/refactor/test/chore。

## 角色代理

`.opencode/agent/` 下预设四类子代理：backend / frontend / devops / doc-writer，派发任务时按域选用。
