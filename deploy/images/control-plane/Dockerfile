# control-plane 单镜像：前端构建 → Go embed 编译 → jammy 运行时（D9/§13.3）
FROM node:22-bookworm-slim AS web
WORKDIR /src/web
RUN npm install -g pnpm@10.33.4
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml web/.npmrc ./
COPY web/apps ./apps
COPY web/packages ./packages
COPY web/internal ./internal
COPY web/scripts ./scripts
COPY web/turbo.json web/vitest.config.ts ./
RUN pnpm install --frozen-lockfile --ignore-scripts \
 && pnpm --filter @vben/web-ipam run typecheck \
 && pnpm run build:ipam

FROM golang:1.27-bookworm AS build
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct \
    GOTOOLCHAIN=auto
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/apps/web-ipam/dist cmd/control-plane/webui/dist/
RUN go build -ldflags "-s -w -X main.version=${VERSION:-0.1.0-dev}" -o /out/control-plane ./cmd/control-plane

FROM ubuntu:jammy
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
 && rm -rf /var/lib/apt/lists/* \
 && useradd -r -u 10001 ipam
COPY --from=build /out/control-plane /usr/local/bin/control-plane
USER ipam
EXPOSE 8443
ENTRYPOINT ["/usr/local/bin/control-plane"]
