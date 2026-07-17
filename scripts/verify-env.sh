#!/usr/bin/env bash
set -euo pipefail

REQUIRED_GO_VERSION="${ZBOARD_REQUIRED_GO_VERSION:-1.26.5}"
ALLOW_STALE="${ZBOARD_ALLOW_STALE_GO_VERSION:-0}"
DISPLAY_TARGET_VERSION="${REQUIRED_GO_VERSION}"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ -f "${script_dir}/ensure-go-env.sh" ]; then
  if [ -n "${ALLOW_STALE}" ] && [ "${ALLOW_STALE}" != "0" ]; then
    export ZBOARD_ALLOW_STALE_GO_VERSION="${ALLOW_STALE}"
  fi
  if [ -n "${REQUIRED_GO_VERSION}" ]; then
    ZBOARD_REQUIRED_GO_VERSION="${REQUIRED_GO_VERSION}" . "${script_dir}/ensure-go-env.sh"
  else
    . "${script_dir}/ensure-go-env.sh"
  fi
else
  echo "ERROR: ensure-go-env.sh missing" >&2
  exit 1
fi

echo "=== zboard environment check ==="
echo "Requested Go target: ${DISPLAY_TARGET_VERSION}"
GO_MOD_PATH="${script_dir}/../backend/go.mod"
if [ -f "${GO_MOD_PATH}" ]; then
  GO_DIRECTIVE="$(awk '/^go[[:space:]]+/{print $2; exit}' "${GO_MOD_PATH}")"
  GO_TOOLCHAIN="$(awk '/^toolchain[[:space:]]+/{print $2; exit}' "${GO_MOD_PATH}")"
  if [ -n "${GO_DIRECTIVE}" ]; then
    echo "Go mod baseline: go ${GO_DIRECTIVE}"
  else
    echo "Go mod baseline: unavailable"
  fi
  if [ -n "${GO_TOOLCHAIN}" ]; then
    echo "Go toolchain baseline: ${GO_TOOLCHAIN}"
  fi
fi
if [ -n "${ZBOARD_GO_VERSION_RESOLVED:-}" ]; then
  echo "Resolved Go target: ${ZBOARD_GO_VERSION_RESOLVED} (${ZBOARD_GO_VERSION_SOURCE:-unknown source})"
elif [ "${DISPLAY_TARGET_VERSION}" != "latest" ]; then
  echo "Resolved Go target: ${DISPLAY_TARGET_VERSION}"
fi

if command -v go >/dev/null 2>&1; then
  go_version="$(go version)"
  echo "go: ${go_version}"
else
  echo "ERROR: go binary still unavailable." >&2
  exit 1
fi

if [ "${ALLOW_STALE}" = "1" ]; then
  echo "Go stale fallback mode: enabled (use local runtime if network unavailable)"
else
  echo "Go stale fallback mode: disabled (require the pinned repository toolchain)"
fi

if command -v pnpm >/dev/null 2>&1; then
  echo "pnpm: $(pnpm --version)"
else
  echo "WARN: pnpm not found. Frontend build will require npm install -g pnpm."
fi

if command -v node >/dev/null 2>&1; then
  echo "node: $(node -v)"
else
  echo "WARN: node not found. Frontend build requires node runtime."
fi

if command -v mysql >/dev/null 2>&1; then
  echo "mysql client: found"
else
  echo "WARN: mysql client not found. DB bootstrap may need mysql installed or docker compose."
fi

if command -v docker >/dev/null 2>&1; then
  echo "docker: $(docker --version)"
else
  echo "WARN: docker not found. Containerized startup path unavailable."
fi

echo "=== check complete ==="
