#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
VERSION_FILE="${ROOT_DIR}/VERSION"
if [ "${ZBOARD_SKIP_GO_BASELINE_CHECK:-0}" = "1" ]; then
  GO_BASELINE_CHECK=0
else
  GO_BASELINE_CHECK=1
fi

"${SCRIPT_DIR}/ensure-go-env.sh"

if [ "${GO_BASELINE_CHECK}" = "1" ]; then
  "${SCRIPT_DIR}/sync-go-baseline.sh" --check-only --target 1.26.5
else
  echo "Go baseline check: skipped."
fi

pushd "${SCRIPT_DIR}/../backend" >/dev/null
if [ -f "${VERSION_FILE}" ]; then
  DEFAULT_VERSION="$(tr -d '\r\n' < "${VERSION_FILE}")"
else
  DEFAULT_VERSION="v0.0.1"
fi
VERSION_TAG="${VERSION_TAG:-${DEFAULT_VERSION}}"
COMMIT_TAG="${COMMIT_TAG:-local}"
BUILD_TIME="${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
go mod tidy
go build -ldflags "-X github.com/zerodenet/zboard/backend/internal/version.Version=${VERSION_TAG} -X github.com/zerodenet/zboard/backend/internal/version.Commit=${COMMIT_TAG} -X github.com/zerodenet/zboard/backend/internal/version.BuildTime=${BUILD_TIME}" -o bin/zboard ./cmd/zboard
popd >/dev/null

pushd "${SCRIPT_DIR}/../frontend" >/dev/null
pnpm install --frozen-lockfile
pnpm build
popd >/dev/null
