# db/ — 数据库结构变更

- `postgresql/migrations/`：幂等迁移脚本（核心模型见架构文档 §3：org_group / asset / coherence_binding / blocklist / policy_group…）
- `clickhouse/`：logs 宽表建表、TTL、物化视图

纪律：迁移必须可重复执行（防 K2）；schema 变更先改 §3 再写迁移。
