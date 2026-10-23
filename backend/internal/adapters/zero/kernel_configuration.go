package zero

// config.apply atomically replaces current.json with a regular file. Preserve
// that file as well as the symlink form used by a fresh installation.
const kernelConfigurationShell = `
backup_current_configuration() {
  old_link=""
  if [ -L /etc/zerodenet/current.json ]; then
    old_config_kind=symlink
    old_link="$(readlink /etc/zerodenet/current.json)"
  elif [ -f /etc/zerodenet/current.json ]; then
    old_config_kind=file
    cp -p /etc/zerodenet/current.json "$backup/runtime.json" || return 1
  elif [ -e /etc/zerodenet/current.json ]; then
    printf 'Unsupported Zero current configuration file type\n' >&2
    return 1
  else
    old_config_kind=missing
  fi
  printf '%s\n' "$old_link" > "$backup/old_link"
  printf '%s\n' "$old_config_kind" > "$backup/old_config_kind"
}
validate_configuration_backup() {
  old_link="$(cat "$backup/old_link")"
  if [ -f "$backup/old_config_kind" ]; then
    old_config_kind="$(cat "$backup/old_config_kind")"
  elif [ -n "$old_link" ]; then
    old_config_kind=symlink
  else
    # An empty legacy link cannot distinguish a missing config from a file.
    printf 'Legacy Zero backup has no recoverable configuration metadata; manual recovery required\n' >&2
    return 1
  fi
  case "$old_config_kind" in
    file) test -f "$backup/runtime.json" || return 1;;
    symlink) test -n "$old_link" || return 1;;
    missing) return 0;;
    *) printf 'Invalid Zero configuration rollback metadata\n' >&2; return 1;;
  esac
}
restore_current_configuration() {
  validate_configuration_backup || return 1
  case "$old_config_kind" in
    file)
      rm -f /etc/zerodenet/current.json.rollback || return 1
      cp -p "$backup/runtime.json" /etc/zerodenet/current.json.rollback || return 1
      mv -Tf /etc/zerodenet/current.json.rollback /etc/zerodenet/current.json || return 1
      ;;
    symlink)
      ln -sfn "$old_link" /etc/zerodenet/current.json.rollback || return 1
      mv -Tf /etc/zerodenet/current.json.rollback /etc/zerodenet/current.json || return 1
      ;;
    missing) rm -f /etc/zerodenet/current.json || return 1;;
    *) printf 'Invalid Zero configuration rollback metadata\n' >&2; return 1;;
  esac
}
`
