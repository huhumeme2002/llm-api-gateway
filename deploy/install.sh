#!/usr/bin/env bash
# Install binary + systemd unit on a Linux VPS.
# Usage (from repo root, as root):
#   ./deploy/install.sh
set -euo pipefail

PREFIX="${PREFIX:-/opt/llmgw}"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) GOARCH=amd64 ;;
  aarch64|arm64) GOARCH=arm64 ;;
  *) echo "unsupported arch: $ARCH" >&2; exit 1 ;;
esac

if [[ "${EUID}" -ne 0 ]]; then
  echo "run as root" >&2
  exit 1
fi

id -u llmgw >/dev/null 2>&1 || useradd --system --home "$PREFIX" --shell /usr/sbin/nologin llmgw
mkdir -p "$PREFIX/data"
if [[ ! -x "$PREFIX/gateway" ]]; then
  if command -v go >/dev/null 2>&1; then
    CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" go build -trimpath -ldflags='-s -w' -o "$PREFIX/gateway" ./cmd/gateway
  else
    echo "place a linux/$GOARCH gateway binary at $PREFIX/gateway or install Go" >&2
    exit 1
  fi
fi
[[ -f "$PREFIX/config.yaml" ]] || cp config.vps.yaml "$PREFIX/config.yaml"
[[ -d "$PREFIX/data" ]] && cp -n data/pricing.yaml "$PREFIX/data/pricing.yaml" || true
[[ -f "$PREFIX/.env" ]] || cp .env.example "$PREFIX/.env"
chown -R llmgw:llmgw "$PREFIX"
chmod 750 "$PREFIX"
chmod 640 "$PREFIX/.env" "$PREFIX/config.yaml"

install -m 0644 deploy/llmgw.service /etc/systemd/system/llmgw.service
systemctl daemon-reload
systemctl enable llmgw
echo "edit $PREFIX/.env then: systemctl start llmgw"
echo "health: curl -s http://127.0.0.1:8080/health"
