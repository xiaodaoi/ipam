#!/bin/sh
# unbound 容器入口（M5-032）：容器内无 cron/systemd，以后台循环每 15min
# 调一次 logrotate（daily/maxsize 判定由其状态文件承担），随后校验并 exec unbound。
set -e
( while :; do logrotate /etc/logrotate.d/unbound || true; sleep 900; done ) &
unbound-checkconf /etc/unbound/unbound.conf
exec unbound -d -c /etc/unbound/unbound.conf
