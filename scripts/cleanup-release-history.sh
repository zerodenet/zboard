#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: bash scripts/cleanup-release-history.sh [--dry-run] TAG

Apply release-history retention after TAG has been published:
  vX.Y.Z-rc[.*]  deletes GitHub Releases and GHCR versions tagged *-dev[.*]
  vX.Y.Z         deletes GitHub Releases and GHCR versions tagged *-rc[.*]

Git tags are intentionally retained. Other prerelease families do not trigger
cleanup.

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
[ -n "${GH_TOKEN:-}" ] || die "GH_TOKEN is required"
[ -n "${GITHUB_REPOSITORY:-}" ] || die "GITHUB_REPOSITORY is required"
[ -n "${GITHUB_REPOSITORY_OWNER:-}" ] || die "GITHUB_REPOSITORY_OWNER is required"

package_name="${ZBOARD_GHCR_PACKAGE:-zboard}"
package_path="orgs/${GITHUB_REPOSITORY_OWNER}/packages/container/${package_name}"
cleanup_re="-${cleanup_family}(\\\\.|$)"
release_jq=".[] | select(.tag_name | test(\"${cleanup_re}\")) | .tag_name"
package_jq=".[] | select((.metadata.container.tags // []) as \$tags | (\$tags | length) > 0 and ([\$tags[] | test(\"${cleanup_re}\")] | any) and ([\$tags[] | test(\"${cleanup_re}\")] | all)) | .id"

echo "Published ${release_tag}; cleaning historical ${cleanup_family} releases and package versions."

mapfile -t release_tags < <(
  gh api --paginate "repos/${GITHUB_REPOSITORY}/releases?per_page=100" --jq "${release_jq}"
)

for tag in "${release_tags[@]}"; do
  if [ "${dry_run}" -eq 1 ]; then
    echo "Would delete GitHub Release ${tag} (Git tag retained)."
  else
    echo "Deleting GitHub Release ${tag} (Git tag retained)."
    gh release delete "${tag}" --repo "${GITHUB_REPOSITORY}" --yes
  fi
done

mapfile -t package_version_ids < <(
  gh api --paginate "${package_path}/versions?per_page=100" --jq "${package_jq}"
)

for version_id in "${package_version_ids[@]}"; do
  if [ "${dry_run}" -eq 1 ]; then
    echo "Would delete GHCR package version ${version_id}."
  else
    echo "Deleting GHCR package version ${version_id}."
    gh api --method DELETE "${package_path}/versions/${version_id}"
  fi
done

echo "Cleanup complete: ${#release_tags[@]} release(s), ${#package_version_ids[@]} package version(s)."
