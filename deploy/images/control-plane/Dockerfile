# control-plane 单镜像：前端构建 → Go embed 编译 → jammy 运行时（D9/§13.3）
FROM node:22-bookworm-slim AS web
ARG APT_MIRROR=""
RUN if [ -n "$APT_MIRROR" ]; then sed -i "s|deb.debian.org|${APT_MIRROR#http*://}|g" /etc/apt/sources.list.d/debian.sources; fi
WORKDIR /src/web
# pnpm 全局安装走阿里源（registry.npmjs.org 在中国网络极慢；业务依赖由 web/.npmrc 管）
RUN npm install -g pnpm@10.33.4 --registry=https://registry.npmmirror.com
ENV IS_CI=true CI=true
# manifest-first：先复制全部 package.json+lock → pnpm install 层只被依赖变更击穿；
# 源码 COPY 在其后，改业务代码不再触发依赖重装（本地调试主通道见 vite.config.ts）
COPY web/package.json web/pnpm-workspace.yaml web/pnpm-lock.yaml web/.npmrc ./
COPY web/apps/web-ipam/package.json ./apps/web-ipam/package.json
COPY web/internal/lint-configs/commitlint-config/package.json ./internal/lint-configs/commitlint-config/package.json
COPY web/internal/lint-configs/eslint-config/package.json ./internal/lint-configs/eslint-config/package.json
COPY web/internal/lint-configs/oxfmt-config/package.json ./internal/lint-configs/oxfmt-config/package.json
COPY web/internal/lint-configs/oxlint-config/package.json ./internal/lint-configs/oxlint-config/package.json
COPY web/internal/lint-configs/stylelint-config/package.json ./internal/lint-configs/stylelint-config/package.json
COPY web/internal/node-utils/package.json ./internal/node-utils/package.json
COPY web/internal/tailwind-config/package.json ./internal/tailwind-config/package.json
COPY web/internal/tsconfig/package.json ./internal/tsconfig/package.json
COPY web/internal/vite-config/package.json ./internal/vite-config/package.json
COPY web/packages/@core/base/design/package.json ./packages/@core/base/design/package.json
COPY web/packages/@core/base/icons/package.json ./packages/@core/base/icons/package.json
COPY web/packages/@core/base/shared/package.json ./packages/@core/base/shared/package.json
COPY web/packages/@core/base/typings/package.json ./packages/@core/base/typings/package.json
COPY web/packages/@core/composables/package.json ./packages/@core/composables/package.json
COPY web/packages/@core/preferences/package.json ./packages/@core/preferences/package.json
COPY web/packages/@core/ui-kit/form-ui/package.json ./packages/@core/ui-kit/form-ui/package.json
COPY web/packages/@core/ui-kit/layout-ui/package.json ./packages/@core/ui-kit/layout-ui/package.json
COPY web/packages/@core/ui-kit/menu-ui/package.json ./packages/@core/ui-kit/menu-ui/package.json
COPY web/packages/@core/ui-kit/popup-ui/package.json ./packages/@core/ui-kit/popup-ui/package.json
COPY web/packages/@core/ui-kit/shadcn-ui/package.json ./packages/@core/ui-kit/shadcn-ui/package.json
COPY web/packages/@core/ui-kit/tabs-ui/package.json ./packages/@core/ui-kit/tabs-ui/package.json
COPY web/packages/constants/package.json ./packages/constants/package.json
COPY web/packages/effects/access/package.json ./packages/effects/access/package.json
COPY web/packages/effects/common-ui/package.json ./packages/effects/common-ui/package.json
COPY web/packages/effects/hooks/package.json ./packages/effects/hooks/package.json
COPY web/packages/effects/layouts/package.json ./packages/effects/layouts/package.json
COPY web/packages/effects/plugins/package.json ./packages/effects/plugins/package.json
COPY web/packages/effects/request/package.json ./packages/effects/request/package.json
COPY web/packages/icons/package.json ./packages/icons/package.json
COPY web/packages/locales/package.json ./packages/locales/package.json
COPY web/packages/preferences/package.json ./packages/preferences/package.json
COPY web/packages/stores/package.json ./packages/stores/package.json
COPY web/packages/styles/package.json ./packages/styles/package.json
COPY web/packages/types/package.json ./packages/types/package.json
COPY web/packages/utils/package.json ./packages/utils/package.json
COPY web/scripts/turbo-run/package.json ./scripts/turbo-run/package.json
COPY web/scripts/vsh/package.json ./scripts/vsh/package.json
RUN pnpm install --frozen-lockfile --ignore-scripts
COPY web/apps ./apps
COPY web/packages ./packages
COPY web/internal ./internal
COPY web/scripts ./scripts
COPY web/turbo.json web/vitest.config.ts ./
# typecheck 由 CI 闸⑤ 承担，镜像内不重复执行（省 vue-tsc 分钟级耗时）
RUN pnpm -r run stub --if-present && pnpm run build:ipam

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
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod go build -ldflags "-s -w -X main.version=${VERSION:-0.1.0-dev}" -o /out/control-plane ./cmd/control-plane

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
 && useradd -r -s /usr/sbin/nologin unbound \
 && useradd -r -u 10001 ipam
COPY --from=build /out/control-plane /usr/local/bin/control-plane
RUN mkdir -p /var/lib/ipam/rpz && chown ipam:ipam /var/lib/ipam/rpz
USER ipam
EXPOSE 8443
ENTRYPOINT ["/usr/local/bin/control-plane"]
