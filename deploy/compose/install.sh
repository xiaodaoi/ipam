#!/usr/bin/env bash
# IPAM compose 一键部署 + 预检（§7/§9：裸 Ubuntu22.04 ≤30min 出服务；M6-001 HTTPS/密钥）
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
env_file="$here/.env"
certs_dir="$here/certs"

fail() { echo "✗ $1"; exit 1; }
ok()   { echo "✓ $1"; }
warn() { echo "⚠ $1"; }

echo "── IPAM 部署预检 ──"
command -v docker >/dev/null || fail "未安装 docker"
docker compose version >/dev/null 2>&1 || fail "docker compose v2 不可用"
command -v openssl >/dev/null || fail "未安装 openssl（用于生成 TLS 证书与 JWT 密钥）"
ok "docker/compose/openssl"

command -v ss >/dev/null && {
  for p in ${IPAM_PORT:-8443}; do
    ss -lun "( sport = :$p )" | grep -q "$p" && fail "端口 $p 已被占用" || ok "端口 $p 空闲"
  done
}

[ -f "$env_file" ] || { cp "$here/.env.example" "$env_file"; echo "→ 已生成 .env"; }
chmod 600 "$env_file"

set_env() {
  local key="$1" val="$2"
  if grep -qE "^${key}=" "$env_file"; then
    # 使用 | 分隔，避免路径/URL 中的 / 冲突
    sed -i "s|^${key}=.*|${key}=${val}|" "$env_file"
  else
    printf '%s=%s\n' "$key" "$val" >> "$env_file"
  fi
}

# 数据库口令：占位值自动替换为随机值（不打印到终端之外的日志）
if grep -qE '^POSTGRES_PASSWORD=(change-me.*)?$' "$env_file"; then
  pg_pw="$(openssl rand -hex 24)"
  set_env POSTGRES_PASSWORD "$pg_pw"
  ok "已生成随机 POSTGRES_PASSWORD（保存在 .env，权限 600）"
fi

# JWT 密钥：缺失/空/占位时生成 64 位 hex；进程重启不会导致全员掉线
jwt_secret="$(grep -E '^IPAM_JWT_SECRET=' "$env_file" | cut -d= -f2- || true)"
if [ -z "$jwt_secret" ] || [[ "$jwt_secret" == change-me* ]]; then
  set_env IPAM_JWT_SECRET "$(openssl rand -hex 32)"
  ok "已生成 IPAM_JWT_SECRET"
fi

# 自签证书：仅用于内网/试用，正式环境请替换为企业 CA 证书
mkdir -p "$certs_dir"
chmod 700 "$certs_dir"
if [ ! -s "$certs_dir/server.crt" ] || [ ! -s "$certs_dir/server.key" ]; then
  host="$(hostname -f 2>/dev/null || echo ipam.local)"
  [ -n "$host" ] || host="ipam.local"
  san="DNS:${host},DNS:localhost,IP:127.0.0.1"
  if [ -n "${IPAM_HOST_IP:-}" ]; then san="${san},IP:${IPAM_HOST_IP}"; fi
  openssl req -x509 -nodes -newkey rsa:2048 -days 825 \
    -subj "/O=IPAM/CN=${host}" \
    -addext "subjectAltName=${san}" \
    -keyout "$certs_dir/server.key" -out "$certs_dir/server.crt" >/dev/null 2>&1 \
    || fail "自签证书生成失败（可手动放置 certs/server.crt + server.key）"
  ok "已生成自签证书 certs/server.crt（CN=${host}）"
  warn "自签证书仅供试用；正式交付请替换为企业 CA 证书并限制私钥权限"
fi
chmod 644 "$certs_dir/server.crt" "$certs_dir/server.key"

set_env IPAM_TLS_CERT "/certs/server.crt"
set_env IPAM_TLS_KEY "/certs/server.key"
set_env IPAM_CERTS_DIR "./certs"
grep -qE '^IPAM_RATE_LIMIT=' "$env_file" || set_env IPAM_RATE_LIMIT "600"

grep -q "change-me" "$env_file" && fail ".env 仍有 change-me 占位值，请检查 POSTGRES_PASSWORD/IPAM_JWT_SECRET"

cd "$here"
docker compose up -d --wait
sleep 1
port="${IPAM_PORT:-8443}"
curl -kfsS "https://127.0.0.1:${port}/healthz" | grep -q '"status":"ok"' \
  && ok "服务就绪：https://127.0.0.1:${port}（自签证书，浏览器需信任或继续访问）" \
  || fail "冒烟失败，查看 docker compose logs control-plane"

# 首次启动的一次性 admin 口令（文件不存在=沿用已有口令）
if docker compose exec -T control-plane test -f /run/ipam/initial-admin-password 2>/dev/null; then
  echo "── 初始管理员口令（仅显示一次，请登录后立即修改）──"
  docker compose exec -T control-plane cat /run/ipam/initial-admin-password
  warn "该文件位于容器 /run/ipam/initial-admin-password；首次登录后建议删除"
else
  echo "→ 管理员口令沿用现有配置（如忘记请在 .env 设置 IPAM_POC_PASSWORD 后重建空库）"
fi
