# coherence-daemon — Go 构建即产物（无前端依赖）
FROM golang:1.27-bookworm AS build
ARG APT_MIRROR=""
RUN if [ -n "$APT_MIRROR" ]; then sed -i "s|deb.debian.org|$APT_MIRROR|g" /etc/apt/sources.list.d/debian.sources; fi
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct \
    GOTOOLCHAIN=auto
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -ldflags "-s -w" -o /out/coherence-daemon ./cmd/coherence-daemon

FROM ubuntu:jammy
ARG APT_MIRROR=""
RUN if [ -n "$APT_MIRROR" ]; then sed -i "s|http://archive.ubuntu.com|$APT_MIRROR|g; s|http://security.ubuntu.com|$APT_MIRROR|g" /etc/apt/sources.list; fi
COPY --from=build /out/coherence-daemon /usr/local/bin/coherence-daemon
RUN mkdir -p /run/ipam
ENTRYPOINT ["/usr/local/bin/coherence-daemon"]
