# unbound — K8 约束：jammy 系统源仅 1.13.1，必须用 NLnetLabs 官方仓库固版 ≥1.16
FROM ubuntu:jammy

RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl gnupg openssl \
    && curl -fsSL https://repo.nlnetlabs.nl/keys/nlnetlabs-keyring.gpg \
        -o /usr/share/keyrings/nlnetlabs-keyring.gpg \
    && echo "deb [signed-by=/usr/share/keyrings/nlnetlabs-keyring.gpg] https://repo.nlnetlabs.nl/ubuntu jammy main" \
        > /etc/apt/sources.list.d/nlnetlabs.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends unbound bind9-dnsutils \
    && rm -rf /var/lib/apt/lists/* \
    && unbound -V | head -1

# 版本断言输出供 CI 日志核对（≥1.16 由 smoke 步骤 grep 强制）
COPY config/unbound/unbound.conf /etc/unbound/unbound.conf
RUN mkdir -p /run/ipam /var/lib/unbound && chown -R unbound:unbound /run/ipam /var/lib/unbound

EXPOSE 53/udp 53/tcp
CMD ["unbound", "-d", "-c", "/etc/unbound/unbound.conf"]
