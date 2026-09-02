#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REQUIRED_GO_VERSION="${ZBOARD_REQUIRED_GO_VERSION:-1.26.5}"
ALLOW_STALE="${ZBOARD_ALLOW_STALE_GO_VERSION:-0}"
QUERY_TIMEOUT="${ZBOARD_GO_QUERY_TIMEOUT:-8}"
QUERY_TIMEOUT_BUDGET="${ZBOARD_GO_QUERY_BUDGET_SEC:-30}"
QUERY_RETRY_LIMIT="${ZBOARD_GO_QUERY_RETRY_LIMIT:-3}"
DOWNLOAD_TIMEOUT="${ZBOARD_GO_DOWNLOAD_TIMEOUT:-120}"
SMOKE_TIMEOUT="${ZBOARD_SMOKE_TIMEOUT:-10}"
FALLBACK_ROOT="${ZBOARD_GOROOT_FALLBACK:-${XDG_DATA_HOME:-${HOME}/.local/share}/zboard/go}"
GO_MOD_PATH="${SCRIPT_DIR}/../backend/go.mod"

if [ "${QUERY_TIMEOUT}" -lt 1 ]; then
  QUERY_TIMEOUT=8
fi
if [ "${QUERY_TIMEOUT_BUDGET}" -lt 1 ]; then
  QUERY_TIMEOUT_BUDGET=30
fi
if [ "${QUERY_RETRY_LIMIT}" -lt 1 ]; then
  QUERY_RETRY_LIMIT=3
fi

echo "=== zboard go version check ==="
echo "Requested Go target: ${REQUIRED_GO_VERSION}"
echo "Go query timeout: ${QUERY_TIMEOUT}s"
echo "Go query budget: ${QUERY_TIMEOUT_BUDGET}s total"
echo "Go query retry limit: ${QUERY_RETRY_LIMIT}"
echo "Go download timeout: ${DOWNLOAD_TIMEOUT}s"
echo "Smoke test timeout: ${SMOKE_TIMEOUT}s"
if [ "${ALLOW_STALE}" = "1" ] || [ "${ALLOW_STALE}" = "true" ] || [ "${ALLOW_STALE}" = "yes" ] || [ "${ALLOW_STALE}" = "on" ]; then
  echo "Go stale fallback mode: enabled (if remote is unavailable)"
else
  echo "Go stale fallback mode: disabled (require the pinned repository toolchain)"
fi
echo "Preferred local SDK root: ${FALLBACK_ROOT}"

if [ -f "${GO_MOD_PATH}" ]; then
  GO_DIRECTIVE="$(awk '/^go[[:space:]]+/{print $2; exit}' "${GO_MOD_PATH}")"
  GO_TOOLCHAIN="$(awk '/^toolchain[[:space:]]+/{print $2; exit}' "${GO_MOD_PATH}")"
  if [ -n "${GO_DIRECTIVE}" ]; then
    echo "go.mod baseline: go ${GO_DIRECTIVE}"
  else
    echo "go.mod baseline: unavailable"
  fi
  if [ -n "${GO_TOOLCHAIN}" ]; then
    echo "go.mod toolchain baseline: ${GO_TOOLCHAIN}"
  fi
fi

echo "--- resolving ..."
export ZBOARD_REQUIRED_GO_VERSION="${REQUIRED_GO_VERSION}"
export ZBOARD_ALLOW_STALE_GO_VERSION="${ALLOW_STALE}"
export ZBOARD_GO_QUERY_TIMEOUT="${QUERY_TIMEOUT}"
export ZBOARD_GO_QUERY_BUDGET_SEC="${QUERY_TIMEOUT_BUDGET}"
export ZBOARD_GO_QUERY_RETRY_LIMIT="${QUERY_RETRY_LIMIT}"
export ZBOARD_GO_DOWNLOAD_TIMEOUT="${DOWNLOAD_TIMEOUT}"

"${SCRIPT_DIR}/verify-env.sh"

if command -v go >/dev/null 2>&1; then
  go_version="$(go version)"
  echo "go executable: ${go_version}"
fi

if [ -n "${ZBOARD_GO_VERSION_RESOLVED:-}" ]; then
  source="${ZBOARD_GO_VERSION_SOURCE:-unknown source}"
  echo "Resolved Go target: ${ZBOARD_GO_VERSION_RESOLVED} (${source})"
else
  echo "Resolved Go target: unavailable"
fi

echo "=== check done ==="
