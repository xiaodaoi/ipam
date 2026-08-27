# control-plane 单镜像：前端构建 → Go embed 编译 → jammy 运行时（D9/§13.3）
FROM node:22-bookworm-slim AS web
ARG APT_MIRROR=""
RUN if [ -n "$APT_MIRROR" ]; then sed -i "s|deb.debian.org|${APT_MIRROR#http*://}|g" /etc/apt/sources.list.d/debian.sources; fi
WORKDIR /src/web
RUN npm install -g pnpm@10.33.4
# 容器内无 git 且不继承宿主 CI 变量：跳过根项目 prepare(lefthook)，否则 install 失败
ENV IS_CI=true CI=true
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml web/.npmrc ./
COPY web/apps ./apps
COPY web/packages ./packages
COPY web/internal ./internal
COPY web/scripts ./scripts
COPY web/turbo.json web/vitest.config.ts ./
RUN pnpm install --frozen-lockfile \
 && pnpm -r run stub --if-present \
 && pnpm --filter @vben/web-ipam run typecheck \
 && pnpm run build:ipam

FROM ubuntu:jammy AS build
ARG APT_MIRROR=""
RUN if [ -n "$APT_MIRROR" ]; then M="${APT_MIRROR#http*://}"; sed -i "s|archive.ubuntu.com|$M|g; s|security.ubuntu.com|$M|g" /etc/apt/sources.list; fi
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl git \
 && rm -rf /var/lib/apt/lists/*
# 不依赖 Docker Hub golang 标签（新版本镜像发布滞后）；版本与 go.mod 对齐
# GO_DL 默认 https://go.dev；中国网络构建传 --build-arg GO_DL=https://golang.google.cn（站点根，不含 /dl）
ARG GO_DL=https://go.dev
RUN curl -fsSL "${GO_DL}/dl/go1.27.0.linux-amd64.tar.gz" | tar -C /usr/local -xz
ENV PATH=/usr/local/go/bin:$PATH \
    GOPATH=/go \
    GOPROXY=https://goproxy.cn,direct \
    GOTOOLCHAIN=local
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/apps/web-ipam/dist cmd/control-plane/webui/dist/
RUN go build -ldflags "-s -w -X main.version=${VERSION:-0.1.0-dev}" -o /out/control-plane ./cmd/control-plane

FROM ubuntu:jammy
ARG APT_MIRROR=""
ARG UNBOUND_VERSION=1.26.0
RUN if [ -n "$APT_MIRROR" ]; then M="${APT_MIRROR#http*://}"; sed -i "s|archive.ubuntu.com|$M|g; s|security.ubuntu.com|$M|g" /etc/apt/sources.list; fi
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl build-essential libssl-dev libexpat1-dev libevent-dev flex bison \
 && curl -fsSL "https://github.com/NLnetLabs/unbound/archive/refs/tags/release-${UNBOUND_VERSION}.tar.gz" -o /tmp/u.tgz \
 && tar -xzf /tmp/u.tgz -C /tmp && cd /tmp/unbound-release-${UNBOUND_VERSION} \
 && ./configure --prefix=/usr --sysconfdir=/etc --with-pidfile=/run/unbound.pid --with-rootkey-file=/var/lib/unbound/root.key \
 && make -j"$(nproc)" && make install && ldconfig \
 && rm -rf /tmp/unbound* \
 && apt-get purge -y build-essential libssl-dev libexpat1-dev libevent-dev flex bison \
 && rm -rf /var/lib/apt/lists/* \
 && useradd -r -u 10001 ipam
COPY --from=build /out/control-plane /usr/local/bin/control-plane
USER ipam
EXPOSE 8443
ENTRYPOINT ["/usr/local/bin/control-plane"]
