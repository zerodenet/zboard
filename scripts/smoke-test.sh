#!/usr/bin/env bash
set -euo pipefail

API_BASE="${API_BASE:-http://127.0.0.1:8080}"
EMAIL="${EMAIL:-admin@zboard.local}"
PASSWORD="${PASSWORD:-${ZBOARD_BOOTSTRAP_ADMIN_PASSWORD:-}}"
REQUEST_TIMEOUT="${ZBOARD_SMOKE_TIMEOUT:-10}"

info() { echo "[INFO] $*"; }
fatal() { echo "[ERROR] $*" >&2; exit 1; }

if [[ -z "${PASSWORD}" ]]; then
  fatal "PASSWORD or ZBOARD_BOOTSTRAP_ADMIN_PASSWORD is required"
fi

if command -v python3 >/dev/null 2>&1; then
  JSON_PARSER=python3
elif command -v python >/dev/null 2>&1; then
  JSON_PARSER=python
else
  fatal "python3 or python is required to parse API responses"
fi

req() {
  local method="$1"
  local path="$2"
  local body=""
  local token=""

  if [[ "$#" -eq 4 ]]; then
    body="$3"
    token="$4"
  elif [[ "$#" -eq 3 ]]; then
    token="$3"
  fi

  local extra=("-s" "-m" "${REQUEST_TIMEOUT}" "-o" "/tmp/zboard_resp.json" "-w" "%{http_code}")
  if [[ -n "$token" ]]; then
    extra+=(-H "Authorization: Bearer $token")
  fi
  extra+=(-X "$method" -H "Content-Type: application/json")
  if [[ -n "$body" ]]; then
    extra+=(-d "$body")
  fi
  local code
  code=$(curl "${extra[@]}" "$API_BASE$path" || true)
  if [[ "$code" != 2* && "$code" != 3* ]]; then
    if [[ -f /tmp/zboard_resp.json ]]; then
      echo "--- response ---"
      cat /tmp/zboard_resp.json
      echo "---------------"
    fi
    fatal "request $method $path failed, status=$code"
  fi
  cat /tmp/zboard_resp.json
}

info "Smoke test for zboard API..."
req GET "/healthz" >/dev/null
info "1) healthz ok"

resp=$(req POST "/api/v1/auth/login" '{"email":"'"$EMAIL"'","password":"'"$PASSWORD"'"}' "")
TOKEN=$(echo "$resp" | "$JSON_PARSER" -c "import json, sys; data = json.load(sys.stdin).get('data') or {}; auth = data.get('auth') or {}; sys.stdout.write(auth.get('token') or '')")
if [[ -z "$TOKEN" ]]; then
  fatal "login failed, no token"
fi
info "2) login ok ($EMAIL)"

req GET "/api/v1/auth/me" "" "$TOKEN" >/dev/null
info "3) auth/me ok"

req GET "/api/v1/plans" "" "$TOKEN" >/dev/null
info "4) plans list ok"

req GET "/api/v1/nodes" "" "$TOKEN" >/dev/null
info "5) nodes list ok"

req GET "/api/v1/traffic/summary" "" "$TOKEN" >/dev/null
info "6) traffic summary ok"

info "Smoke test done."
