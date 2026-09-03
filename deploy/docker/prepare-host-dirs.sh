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
zero_event_spool_dir=$(resolve_path "${ZBOARD_ZERO_EVENT_SPOOL_HOST_DIR:-./zero-events}")
zero_event_spool_blue_dir=$(resolve_path "${ZBOARD_ZERO_EVENT_SPOOL_BLUE_HOST_DIR:-./zero-events-blue}")
zero_event_spool_green_dir=$(resolve_path "${ZBOARD_ZERO_EVENT_SPOOL_GREEN_HOST_DIR:-./zero-events-green}")

mkdir -p "$artifact_dir/rules" "$managed_rule_dir" "$zero_event_spool_dir" "$zero_event_spool_blue_dir" "$zero_event_spool_green_dir"
chmod 0755 "$artifact_dir" "$artifact_dir/rules"
chmod 0750 "$managed_rule_dir" "$zero_event_spool_dir" "$zero_event_spool_blue_dir" "$zero_event_spool_green_dir"

printf 'Prepared trusted artifact directory: %s (read-only mount)\n' "$artifact_dir"
printf 'Prepared managed rule directory: %s (writable persistent mount)\n' "$managed_rule_dir"
printf 'Prepared Zero event spool directory: %s (writable persistent mount)\n' "$zero_event_spool_dir"
printf 'Prepared blue Zero event spool directory: %s (writable persistent mount)\n' "$zero_event_spool_blue_dir"
printf 'Prepared green Zero event spool directory: %s (writable persistent mount)\n' "$zero_event_spool_green_dir"
if [ "${ZBOARD_DATABASE_DRIVER:-mysql}" = "sqlite" ]; then
  database_dir=$(resolve_path "${ZBOARD_DATABASE_HOST_DIR:-./data}")
  mkdir -p "$database_dir"
  chmod 0750 "$database_dir"
  printf 'Prepared SQLite database directory: %s (writable persistent mount)\n' "$database_dir"
fi
printf 'Back up the database, managed rule, and Zero event spool directories together.\n'
