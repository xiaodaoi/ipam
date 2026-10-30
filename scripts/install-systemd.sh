#!/usr/bin/env bash
# 安装 systemd 自启 + 看门狗（M6-003A）
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
dst=/etc/systemd/system
[ "$(id -u)" -eq 0 ] || { echo "请用 root/sudo 执行"; exit 1; }
for f in ipam.service ipam-watchdog.service ipam-watchdog.timer; do
  sed "s#__IPAM_DIR__#$root#g" "$root/deploy/systemd/$f" > "$dst/$f"
done
chmod +x "$root/deploy/systemd/ipam-watchdog.sh"
systemctl daemon-reload
systemctl enable --now ipam.service ipam-watchdog.timer
systemctl --no-pager status ipam.service ipam-watchdog.timer || true
echo "OK: IPAM 自启 + 每分钟看门狗已启用"
