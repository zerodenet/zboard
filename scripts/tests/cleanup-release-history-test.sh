#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="${repo_root}/scripts/cleanup-release-history.sh"
test_dir="$(mktemp -d)"
trap 'rm -rf "${test_dir}"' EXIT

cat > "${test_dir}/gh" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail

printf '%s\n' "$*" >> "${GH_MOCK_LOG}"

if [ "$1" = "api" ] && [[ "$*" == *"/releases?per_page=100"* ]]; then
  if [[ "$*" == *"-dev"* ]]; then
    printf '%s\n' v1.0.0-dev v1.0.0-dev.2
  else
    printf '%s\n' v1.0.0-rc v1.0.0-rc.3
  fi
elif [ "$1" = "api" ] && [[ "$*" == *"/versions?per_page=100"* ]]; then
  if [[ "$*" == *"-dev"* ]]; then
    printf '%s\n' 101 102
  else
    printf '%s\n' 201 202
  fi
fi
MOCK
chmod +x "${test_dir}/gh"

export PATH="${test_dir}:${PATH}"
export GH_TOKEN=test-token
export GITHUB_REPOSITORY=zerodenet/zboard
export GITHUB_REPOSITORY_OWNER=zerodenet
export GH_MOCK_LOG="${test_dir}/gh.log"

assert_log_contains() {
  grep -F -- "$1" "${GH_MOCK_LOG}" >/dev/null || {
    echo "missing expected mock call: $1" >&2
    exit 1
  }
}

: > "${GH_MOCK_LOG}"
bash "${script}" v1.0.0-rc.1
assert_log_contains "release delete v1.0.0-dev --repo zerodenet/zboard --yes"
assert_log_contains "release delete v1.0.0-dev.2 --repo zerodenet/zboard --yes"
assert_log_contains "api --method DELETE orgs/zerodenet/packages/container/zboard/versions/101"
assert_log_contains "api --method DELETE orgs/zerodenet/packages/container/zboard/versions/102"
if grep -F -- "-rc(" "${GH_MOCK_LOG}" >/dev/null; then
  echo "rc publication selected rc history instead of dev history" >&2
  exit 1
fi

: > "${GH_MOCK_LOG}"
bash "${script}" v1.0.0
assert_log_contains "release delete v1.0.0-rc --repo zerodenet/zboard --yes"
assert_log_contains "release delete v1.0.0-rc.3 --repo zerodenet/zboard --yes"
assert_log_contains "api --method DELETE orgs/zerodenet/packages/container/zboard/versions/201"
assert_log_contains "api --method DELETE orgs/zerodenet/packages/container/zboard/versions/202"

: > "${GH_MOCK_LOG}"
output="$(bash "${script}" v1.0.0-dev.9)"
[ "${output}" = "No history cleanup is defined for v1.0.0-dev.9." ]
[ ! -s "${GH_MOCK_LOG}" ]

echo "cleanup-release-history tests passed"
