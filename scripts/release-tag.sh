#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: bash scripts/release-tag.sh [--dry-run] VERSION

Create a release commit, annotated tag, and push the current branch plus tag to
every configured remote.

Examples:
  bash scripts/release-tag.sh 0.0.1
  bash scripts/release-tag.sh 0.1.0-rc
  bash scripts/release-tag.sh --dry-run 0.0.1

Branch policy:
  develop: VERSION must be numeric SemVer, e.g. 0.0.1. The tag becomes
           v0.0.1-dev.YYYYMMDDHHmm using the current UTC minute.
  main:    VERSION may be a numeric SemVer for a formal release, e.g. 0.1.0,
           or an RC base such as 0.1.0-rc. RC tags become
           v0.1.0-rc.YYYYMMDDHHmm; formal tags remain exactly vVERSION.

An existing tag is always an error. The script never infers or appends a
numeric release suffix.

Environment:
  ZBOARD_RELEASE_TIMESTAMP  Override the dev/RC UTC timestamp for a replay or
                            deterministic dry run; must be YYYYMMDDHHmm.
EOF
}

die() {
  echo "release-tag: $*" >&2
  exit 1
}

dry_run=0
version_input=""

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
      if [ -n "${version_input}" ]; then
        die "only one VERSION argument is supported"
      fi
      version_input="$1"
      shift
      ;;
  esac
done

[ -n "${version_input}" ] || {
  usage >&2
  exit 1
}

repo_root="$(git rev-parse --show-toplevel 2>/dev/null)" || die "not inside a git repository"
cd "${repo_root}"

branch="$(git symbolic-ref --quiet --short HEAD 2>/dev/null || true)"
[ -n "${branch}" ] || die "release tagging requires a named branch, not a detached HEAD"

main_branch="${ZBOARD_RELEASE_MAIN_BRANCH:-main}"
develop_branch="${ZBOARD_RELEASE_DEVELOP_BRANCH:-develop}"
numeric_semver_re='^[0-9]+\.[0-9]+\.[0-9]+$'
rc_semver_re='^[0-9]+\.[0-9]+\.[0-9]+-rc$'
version="${version_input#v}"

release_timestamp() {
  local value="${ZBOARD_RELEASE_TIMESTAMP:-$(date -u +%Y%m%d%H%M)}"
  if [[ ! "${value}" =~ ^[0-9]{12}$ ]]; then
    die "release timestamp must use UTC YYYYMMDDHHmm format"
  fi
  printf '%s\n' "${value}"
}

case "${branch}" in
  "${develop_branch}")
    if [[ ! "${version}" =~ ${numeric_semver_re} ]]; then
      die "${develop_branch} only accepts numeric SemVer such as 0.0.1; prerelease suffixes must be created from ${main_branch}"
    fi
    tag="v${version}-dev.$(release_timestamp)"
    ;;
  "${main_branch}")
    if [[ "${version}" =~ ${numeric_semver_re} ]]; then
      tag="v${version}"
    elif [[ "${version}" =~ ${rc_semver_re} ]]; then
      tag="v${version}.$(release_timestamp)"
    else
      die "${main_branch} accepts a formal SemVer such as 0.1.0 or an RC base such as 0.1.0-rc"
    fi
    ;;
  *)
    die "unsupported branch ${branch}; release tags may be created only from ${develop_branch} or ${main_branch}"
    ;;
esac

if [ "${dry_run}" -eq 0 ]; then
  git diff --quiet || die "working tree has unstaged changes; commit or stash them before releasing"
  git diff --cached --quiet || die "index has staged changes; commit or unstage them before releasing"
fi

mapfile -t remotes < <(git remote)
if [ "${dry_run}" -eq 0 ] && [ "${#remotes[@]}" -gt 0 ]; then
  for remote in "${remotes[@]}"; do
    git fetch --tags "${remote}"
  done
fi

tag_exists() {
  git rev-parse --verify --quiet "refs/tags/$1" >/dev/null
}

tag_exists "${tag}" && die "tag ${tag} already exists; choose another explicit version or wait for the next UTC minute"

package_version="${tag#v}"
tracked_version_files=(
  "VERSION"
  "backend/internal/version/version.go"
  "frontend/package.json"
)

echo "Branch: ${branch}"
echo "Input version: ${version}"
echo "Release tag: ${tag}"

if [ "${dry_run}" -eq 1 ]; then
  echo "Dry run: no files, commits, tags, or remotes changed."
  exit 0
fi

RELEASE_TAG="${tag}" FRONTEND_VERSION="${package_version}" node <<'NODE'
const fs = require("fs");

const tag = process.env.RELEASE_TAG;
const frontendVersion = process.env.FRONTEND_VERSION;

function writeIfChanged(path, content) {
  const previous = fs.existsSync(path) ? fs.readFileSync(path, "utf8") : "";
  if (previous !== content) {
    fs.writeFileSync(path, content, "utf8");
  }
}

writeIfChanged("VERSION", `${tag}\n`);

const versionGoPath = "backend/internal/version/version.go";
const versionGo = fs.readFileSync(versionGoPath, "utf8");
const nextVersionGo = versionGo.replace(/Version\s*=\s*"[^"]*"/, `Version   = "${tag}"`);
if (nextVersionGo === versionGo && !versionGo.includes(`Version   = "${tag}"`)) {
  throw new Error("unable to update backend/internal/version/version.go Version");
}
writeIfChanged(versionGoPath, nextVersionGo);

const packagePath = "frontend/package.json";
const packageJson = JSON.parse(fs.readFileSync(packagePath, "utf8"));
packageJson.version = frontendVersion;
writeIfChanged(packagePath, `${JSON.stringify(packageJson, null, 2)}\n`);
NODE

git add "${tracked_version_files[@]}"
if ! git diff --cached --quiet; then
  git commit -m "chore: release ${tag}"
else
  echo "Version files already match ${tag}; no release commit created."
fi

git tag -a "${tag}" -m "release ${tag}"

if [ "${#remotes[@]}" -eq 0 ]; then
  echo "No git remotes configured; tag ${tag} remains local."
  exit 0
fi

for remote in "${remotes[@]}"; do
  git push "${remote}" "${branch}"
  git push "${remote}" "${tag}"
done

echo "Release tag ${tag} pushed to ${#remotes[@]} remote(s)."
