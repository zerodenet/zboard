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

if [ "${GH_MOCK_FAIL:-}" = "refs" ] && [[ "$*" == *"/git/matching-refs/"* ]]; then
  exit 1
fi
if [ "$1" = "api" ] && [[ "$*" == *"/releases?per_page=100"* ]]; then
  printf '%s\n' '[[{"tag_name":"v1.0.0-dev"},{"tag_name":"v1.0.0-dev.2"},{"tag_name":"v1.0.0-rc"},{"tag_name":"v1.0.0-rc.3"},{"tag_name":"v2.0.0-dev.1"}]]'
elif [ "$1" = "api" ] && [[ "$*" == *"/git/matching-refs/"* ]]; then
  printf '%s\n' '[[{"ref":"refs/tags/v1.0.0-dev"},{"ref":"refs/tags/v1.0.0-dev.2"},{"ref":"refs/tags/v1.0.0-rc"},{"ref":"refs/tags/v1.0.0-rc.3"},{"ref":"refs/tags/v2.0.0-dev.1"},{"ref":"refs/tags/sdk/v0.1.0"}]]'
elif [ "$1" = "api" ] && [[ "$*" == *"/versions?per_page=100"* ]]; then
  printf '%s\n' '[[{"id":101,"metadata":{"container":{"tags":["v1.0.0-dev"]}}},{"id":102,"metadata":{"container":{"tags":["v1.0.0-dev.2"]}}},{"id":201,"metadata":{"container":{"tags":["v1.0.0-rc"]}}},{"id":202,"metadata":{"container":{"tags":["v1.0.0-rc.3"]}}},{"id":301,"metadata":{"container":{"tags":["v1.0.0-dev","latest"]}}}]]'
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
assert_log_contains "api --method DELETE repos/zerodenet/zboard/git/refs/tags/v1.0.0-dev"
if grep -E -- '(release delete v1.0.0-rc|DELETE .*versions/301|DELETE .*v2.0.0|DELETE .*sdk/)' "${GH_MOCK_LOG}" >/dev/null; then
  echo "rc publication selected protected history" >&2
  exit 1
fi

: > "${GH_MOCK_LOG}"
bash "${script}" v1.0.0
assert_log_contains "release delete v1.0.0-rc --repo zerodenet/zboard --yes"
assert_log_contains "release delete v1.0.0-rc.3 --repo zerodenet/zboard --yes"
assert_log_contains "api --method DELETE orgs/zerodenet/packages/container/zboard/versions/201"
assert_log_contains "api --method DELETE orgs/zerodenet/packages/container/zboard/versions/202"
assert_log_contains "api --method DELETE repos/zerodenet/zboard/git/refs/tags/v1.0.0-rc.3"

: > "${GH_MOCK_LOG}"
bash "${script}" --dry-run v1.0.0-rc.1 >/dev/null
if grep -E -- '(release delete|--method DELETE)' "${GH_MOCK_LOG}" >/dev/null; then
  echo "dry run mutated history" >&2
  exit 1
fi

: > "${GH_MOCK_LOG}"
if GH_MOCK_FAIL=refs bash "${script}" v1.0.0-rc.1 >/dev/null 2>&1; then
  echo "failed inventory was treated as empty success" >&2
  exit 1
fi
if grep -E -- '(release delete|--method DELETE)' "${GH_MOCK_LOG}" >/dev/null; then
  echo "failed inventory deleted partial history" >&2
  exit 1
fi

: > "${GH_MOCK_LOG}"
output="$(bash "${script}" v1.0.0-dev.9)"
[ "${output}" = "No history cleanup is defined for v1.0.0-dev.9." ]
[ ! -s "${GH_MOCK_LOG}" ]

echo "cleanup-release-history tests passed"
