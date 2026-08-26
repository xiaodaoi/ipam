# coherence-daemon — Go 构建即产物（无前端依赖）
FROM golang:1.27-bookworm AS build
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct \
    GOTOOLCHAIN=auto
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -ldflags "-s -w" -o /out/coherence-daemon ./cmd/coherence-daemon

FROM ubuntu:jammy
COPY --from=build /out/coherence-daemon /usr/local/bin/coherence-daemon
RUN mkdir -p /run/ipam
ENTRYPOINT ["/usr/local/bin/coherence-daemon"]
