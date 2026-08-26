#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="${repo_root}/scripts/release-tag.sh"
test_root="$(mktemp -d)"
trap 'rm -rf -- "${test_root}"' EXIT

fail() {
  echo "release-tag test: $*" >&2
  exit 1
}

new_repo() {
  local branch="$1"
  local path="${test_root}/$2"
  mkdir -p "${path}"
  git -C "${path}" init -q -b "${branch}"
  git -C "${path}" config user.name "Release Test"
  git -C "${path}" config user.email "release-test@example.invalid"
  mkdir -p "${path}/backend/internal/version" "${path}/frontend"
  printf 'test\n' > "${path}/README.md"
  printf 'v0.0.1\n' > "${path}/VERSION"
  printf 'package version\n\nconst Version   = "v0.0.1"\n' > "${path}/backend/internal/version/version.go"
  printf '{"name":"release-test","version":"0.0.1"}\n' > "${path}/frontend/package.json"
  git -C "${path}" add README.md VERSION backend/internal/version/version.go frontend/package.json
  git -C "${path}" commit -q -m init
  printf '%s\n' "${path}"
}

develop_repo="$(new_repo develop develop)"
develop_output="$(cd "${develop_repo}" && ZBOARD_RELEASE_TIMESTAMP=202608261230 bash "${script}" --dry-run 0.0.1)"
grep -q 'Release tag: v0.0.1-dev.202608261230' <<<"${develop_output}" || fail "develop tag does not use UTC minute format"

git -C "${develop_repo}" tag v0.0.1-dev.202608261230
if collision_output="$(cd "${develop_repo}" && ZBOARD_RELEASE_TIMESTAMP=202608261230 bash "${script}" --dry-run 0.0.1 2>&1)"; then
  fail "existing timestamp tag was accepted"
fi
grep -q 'tag v0.0.1-dev.202608261230 already exists' <<<"${collision_output}" || fail "existing tag error is unclear"

if invalid_output="$(cd "${develop_repo}" && ZBOARD_RELEASE_TIMESTAMP=20260826123 bash "${script}" --dry-run 0.0.1 2>&1)"; then
  fail "invalid timestamp was accepted"
fi
grep -q 'UTC YYYYMMDDHHmm' <<<"${invalid_output}" || fail "invalid timestamp error is unclear"

publish_repo="$(new_repo develop publish)"
publish_output="$(cd "${publish_repo}" && ZBOARD_RELEASE_TIMESTAMP=202608261231 bash "${script}" 0.0.1)"
grep -q 'Release tag: v0.0.1-dev.202608261231' <<<"${publish_output}" || fail "develop release did not publish the timestamp tag"
git -C "${publish_repo}" rev-parse --verify refs/tags/v0.0.1-dev.202608261231 >/dev/null || fail "timestamp tag was not created"
grep -qx 'v0.0.1-dev.202608261231' "${publish_repo}/VERSION" || fail "VERSION was not updated"
grep -q 'Version   = "v0.0.1-dev.202608261231"' "${publish_repo}/backend/internal/version/version.go" || fail "backend version was not updated"
grep -q '"version": "0.0.1-dev.202608261231"' "${publish_repo}/frontend/package.json" || fail "frontend version was not updated"
test "$(git -C "${publish_repo}" log -1 --pretty=%s)" = 'chore: release v0.0.1-dev.202608261231' || fail "release commit message is incorrect"

main_repo="$(new_repo main main)"
main_output="$(cd "${main_repo}" && ZBOARD_RELEASE_TIMESTAMP=202608261232 bash "${script}" --dry-run 0.1.0-rc)"
grep -q 'Release tag: v0.1.0-rc.202608261232' <<<"${main_output}" || fail "RC tag does not use UTC minute format"

git -C "${main_repo}" tag v0.1.0-rc.202608261232
if main_collision="$(cd "${main_repo}" && ZBOARD_RELEASE_TIMESTAMP=202608261232 bash "${script}" --dry-run 0.1.0-rc 2>&1)"; then
  fail "existing main tag was accepted"
fi
grep -q 'tag v0.1.0-rc.202608261232 already exists' <<<"${main_collision}" || fail "main collision error is unclear"

if numbered_rc="$(cd "${main_repo}" && bash "${script}" --dry-run 0.1.0-rc.1 2>&1)"; then
  fail "numbered RC input was accepted"
fi
grep -q 'RC base such as 0.1.0-rc' <<<"${numbered_rc}" || fail "numbered RC rejection is unclear"

stable_repo="$(new_repo main stable)"
stable_output="$(cd "${stable_repo}" && ZBOARD_RELEASE_TIMESTAMP=202608261233 bash "${script}" --dry-run 0.1.0)"
grep -q 'Release tag: v0.1.0' <<<"${stable_output}" || fail "formal tag unexpectedly gained a timestamp"

echo "release-tag tests passed"
