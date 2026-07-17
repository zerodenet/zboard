#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REQUIRED_VERSION="${ZBOARD_REQUIRED_GO_VERSION:-1.26.5}"
FALLBACK="${ZBOARD_GOROOT_FALLBACK:-/mnt/c/Users/higanbana/sdk/golang}"
AUTO_INSTALL="${ZBOARD_AUTO_INSTALL_GO:-1}"
ALLOW_STALE_VERSION="${ZBOARD_ALLOW_STALE_GO_VERSION:-0}"
DOWNLOAD_BASE="${ZBOARD_GO_DOWNLOAD_BASE:-https://go.dev/dl}"
REQUEST_TIMEOUT="${ZBOARD_GO_QUERY_TIMEOUT:-8}"
QUERY_TIMEOUT_BUDGET="${ZBOARD_GO_QUERY_BUDGET_SEC:-30}"
QUERY_RETRY_LIMIT="${ZBOARD_GO_QUERY_RETRY_LIMIT:-3}"
DOWNLOAD_TIMEOUT="${ZBOARD_GO_DOWNLOAD_TIMEOUT:-120}"

if [ "${REQUEST_TIMEOUT}" -lt 1 ]; then
  REQUEST_TIMEOUT=8
fi
if [ "${QUERY_TIMEOUT_BUDGET}" -lt 1 ]; then
  QUERY_TIMEOUT_BUDGET=30
fi
if [ "${QUERY_RETRY_LIMIT}" -lt 1 ]; then
  QUERY_RETRY_LIMIT=3
fi
if [ "${DOWNLOAD_TIMEOUT}" -lt 1 ]; then
  DOWNLOAD_TIMEOUT=120
fi

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
  local latest
  local timeout=0

  if ! _begin_query; then
    echo ""
    return
  fi
  timeout="${QUERY_CURRENT_TIMEOUT}"
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
    with urllib.request.urlopen(api, timeout=timeout) as resp:
        data = json.load(resp)
    for item in data:
        if item.get("stable"):
            v = item.get("version", "")
            if v.startswith("go"):
                print(v[2:])
                break
except Exception:
    pass
PY
 )"
  fi

  if [ -z "${payload}" ]; then
    echo ""
    return
  fi

  latest="$(printf '%s\n' "${payload}" \
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
  echo "${latest}"
}

install_go_sdk() {
  local version="$1"
  local install_root="$2"
  local tmpdir
  local version_tag="${version#go}"
  local url="${DOWNLOAD_BASE}/go${version_tag}.linux-amd64.tar.gz"
  local archive

  if ! command -v curl >/dev/null 2>&1; then
    echo "curl is required for auto install." >&2
    exit 1
  fi
  if ! command -v tar >/dev/null 2>&1; then
    echo "tar is required for auto install." >&2
    exit 1
  fi

  tmpdir="$(mktemp -d)"
  archive="${tmpdir}/go.tar.gz"

  echo "Install Go ${version_tag} to ${install_root}"
  if ! curl -fL --max-time "${DOWNLOAD_TIMEOUT}" "${url}" -o "${archive}"; then
    echo "Failed to download ${url}. If downloads are blocked, set ZBOARD_AUTO_INSTALL_GO=0 and preinstall a working go binary in ${install_root}/bin, or set ZBOARD_REQUIRED_GO_VERSION to a local version." >&2
    rm -rf "${tmpdir}"
    exit 1
  fi
  rm -rf "${install_root}"
  mkdir -p "${tmpdir}/unpacked"
  tar -C "${tmpdir}/unpacked" -xzf "${archive}"
  mkdir -p "${install_root}"
  cp -r "${tmpdir}/unpacked/go/"* "${install_root}/"
  rm -rf "${tmpdir}"
}

parse_go_version() {
  if [[ "$1" =~ go([0-9]+)\.([0-9]+)\.([0-9]+)? ]]; then
    echo "${BASH_REMATCH[1]} ${BASH_REMATCH[2]} ${BASH_REMATCH[3]:-0}"
    return 0
  fi
  if [[ "$1" =~ go([0-9]+)\.([0-9]+) ]]; then
    echo "${BASH_REMATCH[1]} ${BASH_REMATCH[2]} 0"
    return 0
  fi
  return 1
}

is_version_below() {
  local major="$1"
  local minor="$2"
  local patch="$3"
  local base_major="$4"
  local base_minor="$5"
  local base_patch="$6"
  if [[ "${major}" -lt "${base_major}" ]]; then
    return 0
  fi
  if [[ "${major}" -eq "${base_major}" && "${minor}" -lt "${base_minor}" ]]; then
    return 0
  fi
  if [[ "${major}" -eq "${base_major}" && "${minor}" -eq "${base_minor}" && "${patch}" -lt "${base_patch}" ]]; then
    return 0
  fi
  return 1
}

ensure_go_sdk() {
  local target_version="$1"
  local force="${2:-0}"
  local go_exe="${FALLBACK}/bin/go"
  local go_exe_win="${FALLBACK}/bin/go.exe"
  local target_major target_minor target_patch
  local fallback_exe=""
  local current_major current_minor current_patch

  if [ -x "${go_exe}" ] || [ -x "${go_exe_win}" ]; then
    if [ "${go_exe_win}" ] && [ -x "${go_exe_win}" ]; then
      fallback_exe="${go_exe_win}"
    else
      fallback_exe="${go_exe}"
    fi

    if [ "${force}" != "1" ]; then
      target_major="$(echo "${target_version#go}" | cut -d. -f1)"
      target_minor="$(echo "${target_version#go}" | cut -d. -f2)"
      target_patch="$(echo "${target_version#go}" | awk -F. '{if (NF>=3) print $3; else print 0}')"
      if [ -z "${target_minor}" ]; then
        target_minor=0
      fi
      if [ -z "${target_patch}" ]; then
        target_patch=0
      fi

      if [ -n "${target_version}" ]; then
        if IFS=' ' read -r current_major current_minor current_patch < <(parse_go_version "$("${fallback_exe}" version)"); then
          if ! is_version_below "${current_major}" "${current_minor}" "${current_patch}" \
            "${target_major}" "${target_minor}" "${target_patch}"; then
            export GOROOT="${FALLBACK}"
            export PATH="${FALLBACK}/bin:${PATH}"
            echo "Switched to fallback SDK at ${FALLBACK}"
            return
          fi
          if [ "${AUTO_INSTALL}" != "1" ]; then
            echo "WARN: fallback Go ${current_major}.${current_minor}.${current_patch} is below required ${target_version}. Auto-install is disabled; using existing fallback."
            export GOROOT="${FALLBACK}"
            export PATH="${FALLBACK}/bin:${PATH}"
            return
          fi
        else
          export GOROOT="${FALLBACK}"
          export PATH="${FALLBACK}/bin:${PATH}"
          echo "Switched to fallback SDK at ${FALLBACK}"
          return
        fi
      else
        export GOROOT="${FALLBACK}"
        export PATH="${FALLBACK}/bin:${PATH}"
        echo "Switched to fallback SDK at ${FALLBACK}"
        return
      fi
      echo "Installed fallback SDK is below required ${target_version}, reinstalling..."
    fi
  else
    if [ "${AUTO_INSTALL}" != "1" ]; then
      echo "Go runtime not found. Expected fallback SDK at ${FALLBACK}." >&2
      exit 1
    fi
  fi

  if [ "${AUTO_INSTALL}" != "1" ]; then
    if [ -x "${go_exe}" ] || [ -x "${go_exe_win}" ]; then
      export GOROOT="${FALLBACK}"
      export PATH="${FALLBACK}/bin:${PATH}"
      echo "Switched to fallback SDK at ${FALLBACK}"
      return
    fi
    echo "Go runtime not found. Expected fallback SDK at ${FALLBACK}." >&2
    exit 1
  fi

  if [ -z "${target_version}" ]; then
    target_version="$(get_latest_go_version)"
  fi
  if [ -z "${target_version}" ]; then
    echo "Unable to determine latest target Go version for SDK installation." >&2
    exit 1
  fi

  # install if force=1 or fallback SDK is missing
  if [ -x "${go_exe}" ] || [ -x "${go_exe_win}" ]; then
    rm -rf "${FALLBACK}"
  fi

  install_go_sdk "${target_version}" "${FALLBACK}"
  export GOROOT="${FALLBACK}"
  export PATH="${FALLBACK}/bin:${PATH}"
  echo "Installed go ${target_version} to fallback SDK: ${FALLBACK}"
}

if [ -z "${REQUIRED_VERSION}" ] || [ "${REQUIRED_VERSION,,}" = "latest" ]; then
  REQUIRED_VERSION="$(get_latest_go_version || true)"
fi

if [ -z "${REQUIRED_VERSION}" ]; then
  if [ "${ALLOW_STALE_VERSION}" = "1" ] && command -v go >/dev/null 2>&1; then
    REQUIRED_VERSION="$(go version | awk '{print $3}' | sed 's/^go//')"
    export ZBOARD_GO_VERSION_SOURCE="stale-local"
    echo "WARN: unable to resolve latest go version from network; allowed stale mode is on, using current local go ${REQUIRED_VERSION}." >&2
  else
    echo "Unable to resolve latest go version from stable index." >&2
    echo "Set ZBOARD_REQUIRED_GO_VERSION manually or set ZBOARD_ALLOW_STALE_GO_VERSION=1 to fallback to local go." >&2
    exit 1
  fi
else
  export ZBOARD_GO_VERSION_SOURCE="remote-stable"
fi

required_major="$(echo "${REQUIRED_VERSION#go}" | cut -d. -f1)"
required_minor="$(echo "${REQUIRED_VERSION#go}" | cut -d. -f2)"
if [ -z "${required_minor}" ]; then
  required_minor=0
fi
required_patch="$(echo "${REQUIRED_VERSION#go}" | awk -F. '{if (NF>=3) print $3; else print 0}')"
if [ -z "${required_patch}" ]; then
  required_patch=0
fi

if ! command -v go >/dev/null 2>&1; then
  ensure_go_sdk "${REQUIRED_VERSION}"
else
  echo "Using PATH go binary: $(command -v go)"
fi

go_version_raw="$(go version)"
if IFS=' ' read -r go_major go_minor go_patch < <(parse_go_version "${go_version_raw}"); then
  if is_version_below "${go_major}" "${go_minor}" "${go_patch}" "${required_major}" "${required_minor}" "${required_patch}"; then
    if [ "${AUTO_INSTALL}" = "1" ]; then
      echo "Current Go ${go_major}.${go_minor}.${go_patch} is below project baseline ${REQUIRED_VERSION}, upgrading fallback SDK..."
      ensure_go_sdk "${REQUIRED_VERSION}" 1
      go_version_raw="$(go version)"
    else
      echo "WARNING: Go version ${go_major}.${go_minor}.${go_patch} is below project minimum ${REQUIRED_VERSION}" >&2
    fi
  else
    :
  fi
else
  echo "WARNING: unable to parse go version: ${go_version_raw}" >&2
fi

export ZBOARD_GO_VERSION_RESOLVED="${REQUIRED_VERSION}"
export ZBOARD_GO_QUERY_BUDGET_SEC="${QUERY_TIMEOUT_BUDGET}"
export ZBOARD_GO_QUERY_RETRY_LIMIT="${QUERY_RETRY_LIMIT}"
echo "Go version check: ${go_version_raw}"
echo "Go query budget: ${REQUEST_TIMEOUT}s/attempt, ${QUERY_TIMEOUT_BUDGET}s total, max ${QUERY_RETRY_LIMIT} attempts."
echo "Project baseline requires Go >= ${REQUIRED_VERSION} (or auto-install path: ${FALLBACK})."
if [ "${ZBOARD_GO_VERSION_SOURCE}" = "remote-stable" ]; then
  echo "Go version resolution: resolved from configured stable index."
elif [ "${ZBOARD_GO_VERSION_SOURCE}" = "stale-local" ]; then
  echo "Go version resolution: using local runtime due stale fallback mode."
fi
