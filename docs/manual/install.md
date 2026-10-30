# 安装部署（单机 Compose）

## 1. 前置条件

- Ubuntu 22.04+（或同代 Linux），Docker 24+ / Compose v2；
- 2C4G / 40GB 起；5000 终端建议 4C8G / 200GB SSD；
- 开放 8443/tcp、53/udp+tcp、67/udp、547/udp；
- 宿主 NTP 已同步（`timedatectl`）。

## 2. 安装

```bash
cd deploy/compose
bash install.sh
```

脚本自动生成 `.env`、JWT 密钥、自签证书，并执行 HTTPS 冒烟。

手动方式：

```bash
cp .env.example .env
# 修改 POSTGRES_PASSWORD、IPAM_JWT_SECRET（≥32 字节随机）
mkdir -p certs && openssl req -x509 -nodes -newkey rsa:2048 -days 825 \
  -subj "/CN=ipam.local" -addext "subjectAltName=DNS:ipam.local,IP:127.0.0.1" \
  -keyout certs/server.key -out certs/server.crt
docker compose up -d --wait
```

## 3. 首次登录

初始 admin 口令在：

- `docker compose logs control-plane` 的 `IPAM INITIAL ADMIN` 段；
- 容器 `/run/ipam/initial-admin-password`（0600）。

默认不强制改密；对外交付前在「系统管理 → 账号安全」开启强制改密。

## 4. 正式证书

替换 `deploy/compose/certs/server.crt|key` 为 CA 签发证书，重启 control-plane：

```bash
docker compose restart control-plane
```

私钥建议 640 且仅运行用户可读。

## 5. 数据与卷

| 卷 | 内容 |
|---|---|
| `pg-data` | PostgreSQL |
| `ch-data` | ClickHouse |
| `ch-exports` | 日志归档 + 备份 |
| `vector-config` | Vector 生成配置 |
| `ipam-run` | 运行时 socket/初始口令 |

备份见 `operations.md`。

## 6. 卸载

```bash
docker compose down          # 保留数据卷
docker compose down -v       # 删除数据（谨慎）
```
