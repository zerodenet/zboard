#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
CONFIG="${1:-etc/zboard.yaml}"

cd "${PROJECT_ROOT}/backend"
exec go run ./cmd/zboard -f "${CONFIG}" -migrate-only
