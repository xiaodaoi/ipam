#!/usr/bin/env bash
# proto 代码生成（钉版本，保证可复现）。依赖：protoc(36.0) 在 PATH，或 ~/.local/bin。
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"

command -v protoc >/dev/null || { echo "FAIL: protoc 未安装（releases/download/v36.0）"; exit 1; }
export PATH="$HOME/go/bin:$PATH"

protoc -I"$root/proto" \
  --go_out="$root/proto/gen" --go_opt=paths=source_relative \
  --go-grpc_out="$root/proto/gen" --go-grpc_opt=paths=source_relative \
  "$root/proto/coherence/v1/coherence.proto"

echo "OK: proto/gen/coherence 已再生"
