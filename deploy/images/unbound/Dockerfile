# unbound — K8 约束：jammy 系统源仅 1.13.1；NLnetLabs apt 源域名在部分网络不可解析，
# 故采用 GitHub Releases 源码钉版自编译（版本完全确定性，满足 ≥1.16）。
FROM ubuntu:jammy
ARG APT_MIRROR=""
RUN if [ -n "$APT_MIRROR" ]; then sed -i "s|http://archive.ubuntu.com|$APT_MIRROR|g; s|http://security.ubuntu.com|$APT_MIRROR|g" /etc/apt/sources.list; fi
ARG UNBOUND_VERSION=1.26.0

RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl bind9-dnsutils file build-essential libssl-dev libexpat1-dev libevent-dev flex bison \
    && curl -fsSL "https://github.com/NLnetLabs/unbound/archive/refs/tags/release-${UNBOUND_VERSION}.tar.gz" \
        -o /tmp/unbound.tgz \
    && tar -xzf /tmp/unbound.tgz -C /tmp \
    && cd "/tmp/unbound-release-${UNBOUND_VERSION}" \
    && ./configure --prefix=/usr --sysconfdir=/etc --localstatedir=/var \
        --with-pidfile=/run/unbound.pid --with-rootkey-file=/var/lib/unbound/root.key \
    && make -j"$(nproc)" \
    && make install \
    && ldconfig \
    && rm -rf /tmp/unbound* /var/lib/apt/lists/*

COPY config/unbound/unbound.conf /etc/unbound/unbound.conf
COPY config/unbound/auth-corp.zone /etc/unbound/auth-corp.zone
RUN useradd -r unbound && mkdir -p /run/ipam /var/lib/unbound \
    && unbound -V | head -2

EXPOSE 53/udp 53/tcp
CMD ["sh", "-c", "unbound-checkconf /etc/unbound/unbound.conf && exec unbound -d -c /etc/unbound/unbound.conf"]
