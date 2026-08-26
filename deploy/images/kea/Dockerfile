# Kea 组件通用镜像（Debian bookworm 官方仓：Kea 2.4，满足 D3 的 2.x 要求）
# 构建参数 COMPONENT ∈ {kea-dhcp4-server, kea-dhcp6-server, kea-ctrl-agent}
FROM debian:bookworm-slim
RUN sed -i 's|deb.debian.org|mirrors.aliyun.com|g' /etc/apt/sources.list.d/debian.sources
ARG COMPONENT
RUN apt-get update \
 && apt-get install -y --no-install-recommends "$COMPONENT" curl \
 && rm -rf /var/lib/apt/lists/* \
 && mkdir -p /var/run/kea /var/lib/kea
