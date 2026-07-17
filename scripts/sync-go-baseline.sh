#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GO_MOD_PATH="${SCRIPT_DIR}/../backend/go.mod"
DOWNLOAD_BASE="${ZBOARD_GO_DOWNLOAD_BASE:-https://go.dev/dl}"
REQUEST_TIMEOUT="${ZBOARD_GO_QUERY_TIMEOUT:-8}"
QUERY_TIMEOUT_BUDGET="${ZBOARD_GO_QUERY_BUDGET_SEC:-30}"
QUERY_RETRY_LIMIT="${ZBOARD_GO_QUERY_RETRY_LIMIT:-3}"
ALLOW_STALE="${ZBOARD_ALLOW_STALE_GO_VERSION:-0}"
DRY_RUN=0
CHECK_ONLY=0
TARGET_VERSION="${ZBOARD_REQUIRED_GO_VERSION:-1.26.5}"

if [ "${REQUEST_TIMEOUT}" -lt 1 ]; then
  REQUEST_TIMEOUT=8
fi
if [ "${QUERY_TIMEOUT_BUDGET}" -lt 1 ]; then
  QUERY_TIMEOUT_BUDGET=30
fi
if [ "${QUERY_RETRY_LIMIT}" -lt 1 ]; then
  QUERY_RETRY_LIMIT=3
fi

usage() {
  cat <<EOF
Usage: ./sync-go-baseline.sh [--go-mod PATH] [--target VERSION] [--download-base URL] [--check-only] [--dry-run]

Options:
  --go-mod PATH             path to go.mod (default: backend/go.mod)
  --target VERSION          explicit target version (e.g. 1.26.5 or go1.26.5), default: 1.26.5
  --download-base URL       go download API base, default: https://go.dev/dl
  --go-query-timeout SEC     per-request query timeout (default: ${REQUEST_TIMEOUT})
  --go-query-budget-sec SEC  total query budget before fallback (default: ${QUERY_TIMEOUT_BUDGET})
  --go-query-retry-limit N   max number of remote probes (default: ${QUERY_RETRY_LIMIT})
  --check-only              fail if baseline not aligned, do not modify files
  --dry-run                 print expected changes without writing
  --help                    show this message
EOF
}

QUERY_START_EPOCH="${SECONDS}"
QUERY_ATTEMPTS=0
QUERY_CURRENT_TIMEOUT=0

_begin_query() {
  if [ "${QUERY_ATTEMPTS}" -ge "${QUERY_RETRY_LIMIT}" ]; then
    return 1
  fi
  QUERY_ATTEMPTS=$((QUERY_ATTEMPTS + 1))

  local elapsed=$((SECONDS - QUERY_START_EPOCH))
  local remaining=$((QUERY_TIMEOUT_BUDGET - elapsed))
  if [ "${remaining}" -le 0 ]; then
    return 1
  fi

  if [ "${remaining}" -lt "${REQUEST_TIMEOUT}" ]; then
    QUERY_CURRENT_TIMEOUT="${remaining}"
  else
    QUERY_CURRENT_TIMEOUT="${REQUEST_TIMEOUT}"
  fi
  return 0
}

get_latest_go_version() {
  local api="${DOWNLOAD_BASE}/?mode=json"
  local payload
  local stable

  if ! _begin_query; then
    echo ""
    return
  fi
  local timeout="${QUERY_CURRENT_TIMEOUT}"
  if command -v curl >/dev/null 2>&1; then
    payload="$(curl -fsSL --max-time "${timeout}" "${api}" || true)"
  elif command -v wget >/dev/null 2>&1; then
    payload="$(wget -qO- --timeout="${timeout}" "${api}" || true)"
  elif command -v python3 >/dev/null 2>&1; then
    payload="$(python3 - "$api" "${timeout}" <<'PY'
import json
import sys
import urllib.request

api = sys.argv[1]
timeout = float(sys.argv[2]) if len(sys.argv) > 2 else 8
try:
    with urllib.request.urlopen(api, timeout=timeout) as fp:
        print(json.dumps(json.load(fp)))
except Exception:
    pass
PY
)"
  fi

  if [ -z "${payload}" ]; then
    echo ""
    return
  fi

  stable="$(printf '%s\n' "${payload}" \
    | tr -d '\n' \
    | awk -F'},{' '
      {
        for (i = 1; i <= NF; i++) {
          obj = $i
          if (obj ~ /"stable"[[:space:]]*:[[:space:]]*true/ && match(obj, /"version"[[:space:]]*:[[:space:]]*"go[0-9]+\.[0-9]+(\.[0-9]+)?"/, m)) {
            ver = m[0]
            sub(/^.*"version"[[:space:]]*:[[:space:]]*"/, "", ver)
            sub(/".*$/, "", ver)
            gsub(/^go/, "", ver)
            print ver
            exit
          }
        }
      }')"
  stable="${stable#go}"
  echo "${stable}"
}

normalize_toolchain() {
  local v="$1"
  if [ -z "${v}" ]; then
    echo ""
    return 1
  fi
  if [[ "${v}" == go* ]]; then
    echo "${v}"
  else
    echo "go${v}"
  fi
}

derive_go_directive() {
  local v="$1"
  local cleaned="${v#go}"
  local major
  local minor
  IFS='.' read -r major minor _ <<< "${cleaned}"
  if [ -z "${major}" ] || [ -z "${minor}" ]; then
    echo ""
    return 1
  fi
  echo "${major}.${minor}"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --go-mod)
      GO_MOD_PATH="$2"
      shift 2
      ;;
    --target)
      TARGET_VERSION="$2"
      shift 2
      ;;
    --download-base)
      DOWNLOAD_BASE="$2"
      shift 2
      ;;
    --go-query-timeout)
      REQUEST_TIMEOUT="$2"
      shift 2
      ;;
    --go-query-budget-sec)
      QUERY_TIMEOUT_BUDGET="$2"
      shift 2
      ;;
    --go-query-retry-limit)
      QUERY_RETRY_LIMIT="$2"
      shift 2
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    --check-only)
      CHECK_ONLY=1
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "unknown option: $1" >&2
      usage
      exit 1
      ;;
  esac
done

if [ "${REQUEST_TIMEOUT}" -lt 1 ]; then
  REQUEST_TIMEOUT=8
fi
if [ "${QUERY_TIMEOUT_BUDGET}" -lt 1 ]; then
  QUERY_TIMEOUT_BUDGET=30
fi
if [ "${QUERY_RETRY_LIMIT}" -lt 1 ]; then
  QUERY_RETRY_LIMIT=3
fi
QUERY_START_EPOCH="${SECONDS}"
QUERY_ATTEMPTS=0

if [ ! -f "${GO_MOD_PATH}" ]; then
  echo "go.mod not found: ${GO_MOD_PATH}" >&2
  exit 1
fi

if [ "${TARGET_VERSION}" = "latest" ]; then
  TARGET_VERSION=""
fi

if [ -z "${TARGET_VERSION}" ]; then
  TARGET_VERSION="$(get_latest_go_version || true)"
  if [ -z "${TARGET_VERSION}" ] && [ "${ALLOW_STALE}" = "1" ] && command -v go >/dev/null 2>&1; then
    TARGET_VERSION="$(go version | awk '{print $3}' | sed 's/^go//')"
    echo "WARN: unable to resolve latest from network; using local go ${TARGET_VERSION} due stale mode."
  fi
fi

if [ -z "${TARGET_VERSION}" ]; then
  echo "Unable to determine target toolchain version." >&2
  exit 1
fi

export ZBOARD_GO_QUERY_BUDGET_SEC="${QUERY_TIMEOUT_BUDGET}"
export ZBOARD_GO_QUERY_RETRY_LIMIT="${QUERY_RETRY_LIMIT}"

TARGET_VERSION="$(normalize_toolchain "${TARGET_VERSION}")"
TARGET_TOOLCHAIN="${TARGET_VERSION}"
TARGET_GO="$(derive_go_directive "${TARGET_TOOLCHAIN}")"
if [ -z "${TARGET_TOOLCHAIN}" ]; then
  echo "Unable to determine target toolchain version." >&2
  exit 1
fi

CURRENT_GO="$(awk '/^go[[:space:]]+/{print $2; exit}' "${GO_MOD_PATH}")"
CURRENT_TOOLCHAIN="$(awk '/^toolchain[[:space:]]+/{print $2; exit}' "${GO_MOD_PATH}")"

if [ "${CURRENT_GO}" = "${TARGET_GO}" ] && [ "${CURRENT_TOOLCHAIN}" = "${TARGET_TOOLCHAIN}" ]; then
  echo "go.mod already aligned: go ${CURRENT_GO}, toolchain ${CURRENT_TOOLCHAIN}."
  exit 0
fi

if [ "${CHECK_ONLY}" = "1" ]; then
  echo "go.mod baseline out of date: target is go ${TARGET_GO}, toolchain ${TARGET_TOOLCHAIN}."
  exit 2
fi

echo "Syncing go.mod baseline:"
echo " - go      ${CURRENT_GO} => ${TARGET_GO}"
echo " - toolchain ${CURRENT_TOOLCHAIN} => ${TARGET_TOOLCHAIN}"

if [ "${DRY_RUN}" = "1" ]; then
  echo "Dry-run mode: no files updated."
  exit 0
fi

TMP_FILE="$(mktemp)"
awk -v target_go="${TARGET_GO}" -v target_toolchain="${TARGET_TOOLCHAIN}" '
{
  if ($0 ~ /^go[[:space:]]+/) {
    print "go " target_go
    next
  }
  if ($0 ~ /^toolchain[[:space:]]+/) {
    print "toolchain " target_toolchain
    next
  }
  print
}' "${GO_MOD_PATH}" > "${TMP_FILE}"

cp "${TMP_FILE}" "${GO_MOD_PATH}"
rm -f "${TMP_FILE}"

echo "Updated go.mod baseline."
