# 升级与回滚

## 升级前

1. 创建备份并下载（系统管理 → 备份与恢复 → 立即备份 → 下载）；
2. 确认新版本镜像可用（`ipam/control-plane:<version>` 等）；
3. 在维护窗口执行（管理面会短暂中断）。

## 升级

```bash
cd deploy/compose
# 可选：升级前快照（或页面操作）
curl -k -X POST https://127.0.0.1:8443/api/v1/system/backups \
  -H "Authorization: Bearer <token>" -H 'Content-Type: application/json' \
  -d '{"note":"pre_upgrade"}'

# 更新 .env 的 IPAM_VERSION 为 x.y.z
docker compose pull || true
docker compose up -d --wait
```

控制面启动时自动执行增量迁移（`db/postgresql/migrations`），失败会保留旧容器；查看日志：

```bash
docker compose logs -f control-plane
```

## 回滚

1. 将 `IPAM_VERSION` 改回上一版本；
2. `docker compose up -d --wait`；
3. 如迁移不可逆，使用升级前备份恢复（系统管理 → 备份与恢复 → 恢复，需确认）。

## 注意

- 迁移只向前，不提供 down；跨大版本升级必须备份；
- 恢复会覆盖 PostgreSQL 与 ClickHouse 日志，需维护窗口；
- 配置目录（Unbound 模板）随仓库版本回退。
