#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
TMP_DIR="${PROJECT_ROOT}/tmp"
mkdir -p "${TMP_DIR}"

API_BASE="http://127.0.0.1:8080"
BACKEND_PORT=8080
FRONTEND_PORT=5173
STARTUP_TIMEOUT=120
GO_QUERY_TIMEOUT=8
GO_DOWNLOAD_TIMEOUT=120
GO_QUERY_TIMEOUT_BUDGET=30
GO_QUERY_RETRY_LIMIT=3
SMOKE_TIMEOUT=10
GO_VERSION="1.26.5"
ENFORCE_GO_BASELINE="${ZBOARD_ENFORCE_GO_BASELINE:-1}"
SKIP_DEPS=0
STOP_WHEN_DONE=0
NO_SMOKE=0
START_FRONTEND=0
API_BASE_MANUALLY_SET=0
DATA_SOURCE="${ZBOARD_LOCAL_DSN:-zboard:zboard-local-db-password@tcp(127.0.0.1:3306)/zboard?charset=utf8mb4&parseTime=true&loc=Local}"
REDIS_ADDR="${ZBOARD_LOCAL_REDIS:-127.0.0.1:6379}"
JWT_SECRET="${ZBOARD_JWT_SECRET:-}"
CREDENTIAL_ENCRYPTION_KEY="${ZBOARD_CREDENTIAL_ENCRYPTION_KEY:-}"
ADMIN_USERNAME="${ZBOARD_BOOTSTRAP_ADMIN_USERNAME:-admin}"
ADMIN_EMAIL="${ZBOARD_BOOTSTRAP_ADMIN_EMAIL:-}"
ADMIN_PASSWORD="${ZBOARD_BOOTSTRAP_ADMIN_PASSWORD:-}"

usage() {
  cat <<EOF
Usage: ./scripts/start-dev.sh [options]

Options:
  --api-base URL        backend API base URL (default: http://127.0.0.1:8080)
  --backend-port PORT   backend port, updates api base when --api-base not set
  --frontend-port PORT  frontend dev port (default: 5173)
  --startup-timeout SEC readiness wait timeout (default: 120)
  --go-query-timeout SEC query timeout for go stable lookup/install checks (default: 8)
  --go-query-budget-sec SEC total budget for repeated go lookup attempts (default: 30)
  --go-query-retry-limit N max attempts across query backends (default: 3)
  --go-download-timeout SEC timeout for go download during auto-install (default: 120)
  --smoke-timeout SEC timeout for smoke-test API calls (default: 10)
  --go-version VERSION      Go version target for baseline resolution, default: 1.26.5
  --datasource DSN      override mysql DSN
  --redis-addr ADDR     override redis addr
  --skip-deps           do not auto-start mysql/redis in docker
  --no-smoke            skip smoke test
  --with-frontend       start frontend dev server
  --stop-when-done      stop services after smoke test and exit
  --help                show this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --api-base)
      if [[ $# -lt 2 ]]; then
        echo "missing value for --api-base" >&2
        usage
        exit 1
      fi
      API_BASE="$2"
      API_BASE_MANUALLY_SET=1
      shift 2
      ;;
    --backend-port)
      if [[ $# -lt 2 ]]; then
        echo "missing value for --backend-port" >&2
        usage
        exit 1
      fi
      BACKEND_PORT="$2"
      if [[ ${API_BASE_MANUALLY_SET} -eq 0 ]]; then
        API_BASE="http://127.0.0.1:${BACKEND_PORT}"
      fi
      shift 2
      ;;
    --frontend-port)
      if [[ $# -lt 2 ]]; then
        echo "missing value for --frontend-port" >&2
        usage
        exit 1
      fi
      FRONTEND_PORT="$2"
      shift 2
      ;;
    --startup-timeout)
      if [[ $# -lt 2 ]]; then
        echo "missing value for --startup-timeout" >&2
        usage
        exit 1
      fi
      STARTUP_TIMEOUT="$2"
      shift 2
      ;;
    --go-query-timeout)
      if [[ $# -lt 2 ]]; then
        echo "missing value for --go-query-timeout" >&2
        usage
        exit 1
      fi
      GO_QUERY_TIMEOUT="$2"
      shift 2
      ;;
    --go-query-budget-sec)
      if [[ $# -lt 2 ]]; then
        echo "missing value for --go-query-budget-sec" >&2
        usage
        exit 1
      fi
      GO_QUERY_TIMEOUT_BUDGET="$2"
      shift 2
      ;;
    --go-query-retry-limit)
      if [[ $# -lt 2 ]]; then
        echo "missing value for --go-query-retry-limit" >&2
        usage
        exit 1
      fi
      GO_QUERY_RETRY_LIMIT="$2"
      shift 2
      ;;
    --go-version)
      if [[ $# -lt 2 ]]; then
        echo "missing value for --go-version" >&2
        usage
        exit 1
      fi
      GO_VERSION="$2"
      shift 2
      ;;
    --go-download-timeout)
      if [[ $# -lt 2 ]]; then
        echo "missing value for --go-download-timeout" >&2
        usage
        exit 1
      fi
      GO_DOWNLOAD_TIMEOUT="$2"
      shift 2
      ;;
    --smoke-timeout)
      if [[ $# -lt 2 ]]; then
        echo "missing value for --smoke-timeout" >&2
        usage
        exit 1
      fi
      SMOKE_TIMEOUT="$2"
      shift 2
      ;;
    --datasource)
      if [[ $# -lt 2 ]]; then
        echo "missing value for --datasource" >&2
        usage
        exit 1
      fi
      DATA_SOURCE="$2"
      shift 2
      ;;
    --redis-addr)
      if [[ $# -lt 2 ]]; then
        echo "missing value for --redis-addr" >&2
        usage
        exit 1
      fi
      REDIS_ADDR="$2"
      shift 2
      ;;
    --skip-deps)
      SKIP_DEPS=1
      shift
      ;;
    --no-smoke)
      NO_SMOKE=1
      shift
      ;;
    --with-frontend)
      START_FRONTEND=1
      shift
      ;;
    --stop-when-done)
      STOP_WHEN_DONE=1
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

info() {
  echo "[INFO] $*"
}
warn() {
  echo "[WARN] $*" >&2
}
fatal() {
  echo "[ERROR] $*" >&2
  exit 1
}

generate_secret() {
  local bytes="${1:-32}"
  od -An -N"${bytes}" -tx1 /dev/urandom | tr -d ' \n'
}

read_stored_secret() {
  local name="$1"
  local file="$2"
  if [[ ! -f "${file}" ]]; then
    return 0
  fi
  awk -F= -v name="${name}" '$1 == name { sub(/^[^=]*=/, ""); print; exit }' "${file}"
}

resolve_frontend_api_base() {
  local base="$1"
  local normalized="${base%/}"
  case "$normalized" in
    */api/v1) ;;
    *) normalized="${normalized}/api/v1" ;;
  esac
  echo "$normalized"
}

VITE_API_BASE="$(resolve_frontend_api_base "${API_BASE}")"

export ZBOARD_GO_QUERY_TIMEOUT="${GO_QUERY_TIMEOUT}"
export ZBOARD_GO_QUERY_BUDGET_SEC="${GO_QUERY_TIMEOUT_BUDGET}"
export ZBOARD_GO_QUERY_RETRY_LIMIT="${GO_QUERY_RETRY_LIMIT}"
export ZBOARD_GO_DOWNLOAD_TIMEOUT="${GO_DOWNLOAD_TIMEOUT}"
export ZBOARD_SMOKE_TIMEOUT="${SMOKE_TIMEOUT}"
export ZBOARD_REQUIRED_GO_VERSION="${GO_VERSION}"

info "Resolved frontend api base: ${VITE_API_BASE}"
GO_MOD_PATH="${PROJECT_ROOT}/backend/go.mod"
if [ -f "${GO_MOD_PATH}" ]; then
  GO_DIRECTIVE="$(awk '/^go[[:space:]]+/{print $2; exit}' "${GO_MOD_PATH}")"
  GO_TOOLCHAIN="$(awk '/^toolchain[[:space:]]+/{print $2; exit}' "${GO_MOD_PATH}")"
  if [ -n "${GO_DIRECTIVE}" ]; then
    info "Go mod baseline: go ${GO_DIRECTIVE}"
  else
    info "Go mod baseline: unavailable"
  fi
  if [ -n "${GO_TOOLCHAIN}" ]; then
    info "Go toolchain baseline: ${GO_TOOLCHAIN}"
  fi
fi

info "=== zboard local startup ==="
info "API base: ${API_BASE}"

"${SCRIPT_DIR}/verify-env.sh"
GO_BASELINE_TARGET="${ZBOARD_GO_VERSION_RESOLVED:-${ZBOARD_REQUIRED_GO_VERSION}}"
if [ -n "${ZBOARD_GO_VERSION_SOURCE:-}" ]; then
  info "Resolved Go target: ${GO_BASELINE_TARGET} (${ZBOARD_GO_VERSION_SOURCE})"
else
  info "Resolved Go target: ${GO_BASELINE_TARGET}"
fi

if [[ "${ENFORCE_GO_BASELINE}" == "1" ]]; then
  info "Checking go.mod baseline strictly before startup."
  "${SCRIPT_DIR}/sync-go-baseline.sh" --check-only --target "${GO_BASELINE_TARGET}"
else
  "${SCRIPT_DIR}/sync-go-baseline.sh" --dry-run --target "${GO_BASELINE_TARGET}" >/dev/null 2>&1 || true
fi

if [[ "${SKIP_DEPS}" == "0" ]] && command -v docker >/dev/null 2>&1; then
  info "Starting dependency services via docker compose (mysql, redis)..."
  ZBOARD_MYSQL_ROOT_PASSWORD="${ZBOARD_MYSQL_ROOT_PASSWORD:-zboard-local-root-password}" \
    ZBOARD_MYSQL_PASSWORD="${ZBOARD_MYSQL_PASSWORD:-zboard-local-db-password}" \
    docker compose -f "${PROJECT_ROOT}/deploy/docker/docker-compose.yml" up -d mysql redis
fi

dev_secrets_path="${TMP_DIR}/zboard.dev.secrets"
if [[ -z "${JWT_SECRET}" ]]; then
  JWT_SECRET="$(read_stored_secret ZBOARD_JWT_SECRET "${dev_secrets_path}")"
  if [[ -z "${JWT_SECRET}" ]]; then
    JWT_SECRET="$(generate_secret 32)"
  fi
fi
if [[ -z "${CREDENTIAL_ENCRYPTION_KEY}" ]]; then
  CREDENTIAL_ENCRYPTION_KEY="$(read_stored_secret ZBOARD_CREDENTIAL_ENCRYPTION_KEY "${dev_secrets_path}")"
  if [[ -z "${CREDENTIAL_ENCRYPTION_KEY}" ]]; then
    CREDENTIAL_ENCRYPTION_KEY="$(generate_secret 32)"
  fi
fi
if [[ -z "${ADMIN_EMAIL}" ]]; then
  ADMIN_EMAIL="${ADMIN_USERNAME}@zboard.local"
fi
generated_admin_password=0
if [[ -z "${ADMIN_PASSWORD}" ]]; then
  ADMIN_PASSWORD="$(read_stored_secret ZBOARD_BOOTSTRAP_ADMIN_PASSWORD "${dev_secrets_path}")"
  if [[ -z "${ADMIN_PASSWORD}" ]]; then
    ADMIN_PASSWORD="$(generate_secret 24)"
    generated_admin_password=1
  fi
fi
umask 077
{
  printf 'ZBOARD_JWT_SECRET=%s\n' "${JWT_SECRET}"
  printf 'ZBOARD_CREDENTIAL_ENCRYPTION_KEY=%s\n' "${CREDENTIAL_ENCRYPTION_KEY}"
  printf 'ZBOARD_BOOTSTRAP_ADMIN_PASSWORD=%s\n' "${ADMIN_PASSWORD}"
} > "${dev_secrets_path}"
info "Local bootstrap admin: ${ADMIN_USERNAME}"
if [[ "${generated_admin_password}" == "1" ]]; then
  info "Generated local bootstrap password: ${ADMIN_PASSWORD}"
  info "Save this value for subsequent runs against the same database."
fi

source_config="${PROJECT_ROOT}/backend/etc/zboard.yaml.example"
runtime_config="${TMP_DIR}/zboard.local.yaml"

awk -v dsn="${DATA_SOURCE}" -v redis_addr="${REDIS_ADDR}" -v backend_port="${BACKEND_PORT}" '
{
  if ($0 ~ /^[[:space:]]*environment:/) {
    print "environment: development"
    next
  }
  if ($0 ~ /^[[:space:]]*datasource:/) {
    print "datasource: \"" dsn "\""
    next
  }
  if ($0 ~ /^[[:space:]]*redis_addr:/) {
    print "redis_addr: \"" redis_addr "\""
    next
  }
  if ($0 ~ /^[[:space:]]*Port:/) {
    print "Port: " backend_port
    next
  }
  print
}' "${source_config}" > "${runtime_config}"

backend_log="${TMP_DIR}/zboard-backend.log"
frontend_log="${TMP_DIR}/zboard-frontend.log"

info "Starting backend..."
(
  cd "${PROJECT_ROOT}/backend"
  ZBOARD_ENVIRONMENT=development \
    ZBOARD_JWT_SECRET="${JWT_SECRET}" \
    ZBOARD_CREDENTIAL_ENCRYPTION_KEY="${CREDENTIAL_ENCRYPTION_KEY}" \
    ZBOARD_BOOTSTRAP_ADMIN_USERNAME="${ADMIN_USERNAME}" \
    ZBOARD_BOOTSTRAP_ADMIN_EMAIL="${ADMIN_EMAIL}" \
    ZBOARD_BOOTSTRAP_ADMIN_PASSWORD="${ADMIN_PASSWORD}" \
    go run ./cmd/zboard -f "${runtime_config}"
) >"${backend_log}" 2>&1 &
BACKEND_PID=$!

ready=0
deadline=$((SECONDS + STARTUP_TIMEOUT))
while (( SECONDS < deadline )); do
  if curl -fsS "${API_BASE}/healthz" >/dev/null 2>&1; then
    ready=1
    break
  fi

  if ! kill -0 "${BACKEND_PID}" 2>/dev/null; then
    fatal "backend exited before readiness. check ${backend_log}"
  fi

  sleep 1
done

if [[ ${ready} -ne 1 ]]; then
  fatal "backend not ready within ${STARTUP_TIMEOUT}s. check ${backend_log}"
fi

info "Backend ready: ${API_BASE}/healthz"

if [[ "${START_FRONTEND}" == "1" ]]; then
  if command -v pnpm >/dev/null 2>&1; then
    info "Starting frontend..."
    (
      cd "${PROJECT_ROOT}/frontend"
      pnpm install
      VITE_API_BASE="${VITE_API_BASE}" pnpm dev --host 0.0.0.0 --port "${FRONTEND_PORT}"
    ) >"${frontend_log}" 2>&1 &
    FRONTEND_PID=$!
    info "Frontend started: http://127.0.0.1:${FRONTEND_PORT}"
  else
    warn "pnpm not found, frontend skipped."
    FRONTEND_PID=""
  fi
else
  FRONTEND_PID=""
fi

if [[ "${NO_SMOKE}" != "1" ]]; then
  API_BASE="${API_BASE}" ACCOUNT="${ADMIN_USERNAME}" PASSWORD="${ADMIN_PASSWORD}" bash "${SCRIPT_DIR}/smoke-test.sh"
fi

if [[ "${STOP_WHEN_DONE}" == "1" ]]; then
  info "Smoke test done, stopping services."
  kill "${BACKEND_PID}" 2>/dev/null || true
  if [[ -n "${FRONTEND_PID}" ]]; then
    kill "${FRONTEND_PID}" 2>/dev/null || true
  fi
  exit 0
fi

trap 'info "Stopping services..."; kill "${BACKEND_PID}" 2>/dev/null || true; if [[ -n "${FRONTEND_PID}" ]]; then kill "${FRONTEND_PID}" 2>/dev/null || true; fi; wait "${BACKEND_PID}" 2>/dev/null || true; if [[ -n "${FRONTEND_PID}" ]]; then wait "${FRONTEND_PID}" 2>/dev/null || true; fi' INT TERM EXIT

info "Services running. Press Ctrl+C to stop."
while true; do
  if ! kill -0 "${BACKEND_PID}" 2>/dev/null; then
    echo "[ERROR] backend process exited unexpectedly."
    exit 1
  fi
  sleep 2
done
