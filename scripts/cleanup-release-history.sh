#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: bash scripts/cleanup-release-history.sh [--dry-run] TAG

Apply release-history retention after TAG has been published:
  vX.Y.Z-rc[.*]  deletes obsolete dev Releases, Git tags and GHCR versions
  vX.Y.Z         deletes obsolete rc Releases, Git tags and GHCR versions

Only product versions at or below X.Y.Z are eligible. Newer versions and
unrelated tags are preserved. Other prerelease families do not trigger cleanup.

Environment:
  GITHUB_REPOSITORY       owner/repository containing the releases
  GITHUB_REPOSITORY_OWNER owner of the GHCR package
  ZBOARD_GHCR_PACKAGE     GHCR package name (default: zboard)
EOF
}

die() {
  echo "cleanup-release-history: $*" >&2
  exit 1
}

dry_run=0
release_tag=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --dry-run)
      dry_run=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    -*)
      die "unknown option: $1"
      ;;
    *)
      [ -z "${release_tag}" ] || die "only one TAG argument is supported"
      release_tag="$1"
      shift
      ;;
  esac
done

[ -n "${release_tag}" ] || {
  usage >&2
  exit 1
}

stable_re='^v[0-9]+\.[0-9]+\.[0-9]+$'
rc_re='^v[0-9]+\.[0-9]+\.[0-9]+-rc(\.[0-9A-Za-z][0-9A-Za-z.-]*)?$'

if [[ "${release_tag}" =~ ${rc_re} ]]; then
  cleanup_family="dev"
elif [[ "${release_tag}" =~ ${stable_re} ]]; then
  cleanup_family="rc"
else
  echo "No history cleanup is defined for ${release_tag}."
  exit 0
fi

command -v gh >/dev/null 2>&1 || die "gh is required"
command -v python3 >/dev/null 2>&1 || die "python3 is required"
[ -n "${GH_TOKEN:-}" ] || die "GH_TOKEN is required"
[ -n "${GITHUB_REPOSITORY:-}" ] || die "GITHUB_REPOSITORY is required"
[ -n "${GITHUB_REPOSITORY_OWNER:-}" ] || die "GITHUB_REPOSITORY_OWNER is required"

package_name="${ZBOARD_GHCR_PACKAGE:-zboard}"
package_path="orgs/${GITHUB_REPOSITORY_OWNER}/packages/container/${package_name}"
policy="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/release_history_policy.py"
inventory="$(mktemp -d)"
trap 'rm -rf "${inventory}"' EXIT

echo "Published ${release_tag}; cleaning historical ${cleanup_family} releases and package versions."

# Complete every inventory before deleting anything. Failed API reads must not
# be swallowed by process substitution and reported as a successful empty plan.
gh api --paginate --slurp "repos/${GITHUB_REPOSITORY}/releases?per_page=100" > "${inventory}/releases.json"
gh api --paginate --slurp "repos/${GITHUB_REPOSITORY}/git/matching-refs/tags/" > "${inventory}/refs.json"
gh api --paginate --slurp "${package_path}/versions?per_page=100" > "${inventory}/packages.json"
for kind in releases refs packages; do
  python3 "${policy}" "${release_tag}" "${kind}" "${inventory}/${kind}.json" > "${inventory}/${kind}.plan"
done

release_count=0
while IFS= read -r tag; do
  [ -n "${tag}" ] || continue
  if [ "${dry_run}" -eq 1 ]; then
    echo "Would delete GitHub Release ${tag}."
  else
    echo "Deleting GitHub Release ${tag}."
    gh release delete "${tag}" --repo "${GITHUB_REPOSITORY}" --yes
  fi
  release_count=$((release_count + 1))
done < "${inventory}/releases.plan"

tag_count=0
while IFS= read -r tag; do
  [ -n "${tag}" ] || continue
  if [ "${dry_run}" -eq 1 ]; then
    echo "Would delete Git tag ${tag}."
  else
    echo "Deleting Git tag ${tag}."
    gh api --method DELETE "repos/${GITHUB_REPOSITORY}/git/refs/tags/${tag}"
  fi
  tag_count=$((tag_count + 1))
done < "${inventory}/refs.plan"

package_count=0
while IFS= read -r version_id; do
  [ -n "${version_id}" ] || continue
  if [ "${dry_run}" -eq 1 ]; then
    echo "Would delete GHCR package version ${version_id}."
  else
    echo "Deleting GHCR package version ${version_id}."
    gh api --method DELETE "${package_path}/versions/${version_id}"
  fi
  package_count=$((package_count + 1))
done < "${inventory}/packages.plan"

echo "Cleanup complete: ${release_count} release(s), ${tag_count} tag(s), ${package_count} package version(s)."
