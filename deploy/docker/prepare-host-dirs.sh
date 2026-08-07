#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

resolve_path() {
  case "$1" in
    /*) printf '%s\n' "$1" ;;
    ./*) printf '%s/%s\n' "$script_dir" "${1#./}" ;;
    *) printf '%s/%s\n' "$script_dir" "$1" ;;
  esac
}

artifact_dir=$(resolve_path "${ZBOARD_ZERO_ARTIFACT_HOST_DIR:-./artifacts}")
managed_rule_dir=$(resolve_path "${ZBOARD_MANAGED_RULE_HOST_DIR:-./managed-rules}")

mkdir -p "$artifact_dir/rules" "$managed_rule_dir"
chmod 0755 "$artifact_dir" "$artifact_dir/rules"
chmod 0750 "$managed_rule_dir"

printf 'Prepared trusted artifact directory: %s (read-only mount)\n' "$artifact_dir"
printf 'Prepared managed rule directory: %s (writable persistent mount)\n' "$managed_rule_dir"
printf 'Back up the managed rule directory together with the Zboard database.\n'
