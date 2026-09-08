#!/bin/sh
# Offline cleanup of the Linux/systemd Zero instance installed by ZBoard.
set -eu

cleanup_root=''
cleanup_binary=/usr/local/bin/zero
cleanup_config=/etc/zerodenet/current.json
cleanup_unit=zero.service

fail() { printf 'Cleanup incomplete: %s\n' "$*" >&2; exit 1; }
ctl() { timeout -k 2 15 systemctl "$@"; }

self_removal_help() {
    printf '%s\n' \
        'The cleanup utility does not delete itself.' \
        'After cleanup is complete, remove the installed utility if no longer needed:' \
        '  sudo rm -- /usr/local/sbin/zboard-zero-cleanup' \
        'For an uploaded copy, remove its actual upload path, for example:' \
        '  rm -- ./cleanup-zero-node.sh' \
        'Deleting this script alone does not stop or uninstall Zero.'
}

is_managed_pid() {
    [ -r "$cleanup_root/proc/$1/cmdline" ] || return 1
    cleanup_exe=$(readlink "$cleanup_root/proc/$1/exe" 2>/dev/null) || return 1
    cleanup_exe=${cleanup_exe% (deleted)}
    [ "$cleanup_exe" = "$cleanup_binary" ] || return 1
    tr '\000' '\n' < "$cleanup_root/proc/$1/cmdline" | grep -Fxq "$cleanup_config"
}
managed_pids() {
    for cleanup_proc in "$cleanup_root"/proc/[0-9]*; do
        cleanup_pid=${cleanup_proc##*/}
        if is_managed_pid "$cleanup_pid"; then printf '%s\n' "$cleanup_pid"; fi
    done
}

verify_unit() {
    cleanup_command=$(ctl show "$cleanup_unit" --property=ExecStart --value) || fail 'cannot query systemd'
    if [ -n "$cleanup_command" ]; then
        printf '%s\n' "$cleanup_command" | grep -Eq '(^|[[:space:]=])/usr/local/bin/zero([[:space:];]|$)' || fail 'service uses another binary'
        printf '%s\n' "$cleanup_command" | grep -Eq '(^|[[:space:]])/etc/zerodenet/current[.]json([[:space:];}]|$)' || fail 'service uses another configuration'
    fi
    cleanup_unit_file="$cleanup_root/etc/systemd/system/$cleanup_unit"
    if [ -f "$cleanup_unit_file" ] && [ ! -L "$cleanup_unit_file" ]; then
        grep -Eq '(^|[[:space:]=])/usr/local/bin/zero([[:space:];]|$)' "$cleanup_unit_file" && grep -Eq '(^|[[:space:]])/etc/zerodenet/current[.]json([[:space:];}]|$)' "$cleanup_unit_file" || fail 'unit file is not owned by the ZBoard installation'
    fi
}
service_active() {
    cleanup_state=$(ctl show "$cleanup_unit" --property=ActiveState --value) || fail 'cannot verify service state'
    case "$cleanup_state" in
        active|activating|deactivating|reloading) return 0;;
        inactive|failed) return 1;;
        *) fail 'unexpected service state';;
    esac
}
wait_managed_processes() {
    cleanup_attempt=0
    while [ -n "$(managed_pids)" ] && [ "$cleanup_attempt" -lt 3 ]; do
        sleep 1
        cleanup_attempt=$((cleanup_attempt + 1))
    done
}
stop_zero() {
    verify_unit
    cleanup_load=$(ctl show "$cleanup_unit" --property=LoadState --value) || fail 'cannot query systemd'
    if [ "$cleanup_load" != not-found ]; then
        ctl disable "$cleanup_unit" >/dev/null || fail 'could not disable service startup'
        ctl stop "$cleanup_unit" >/dev/null 2>&1 || :
        if service_active; then
            ctl kill --signal=KILL "$cleanup_unit" || fail 'could not kill the stalled service'
            ctl stop "$cleanup_unit" >/dev/null 2>&1 || :
        fi
    fi
    # Recheck the exact executable and config before signalling each PID.
    for cleanup_signal in TERM KILL; do
        for cleanup_pid in $(managed_pids); do
            if is_managed_pid "$cleanup_pid"; then kill -"$cleanup_signal" "$cleanup_pid" 2>/dev/null || :; fi
        done
        wait_managed_processes
    done
    [ -z "$(managed_pids)" ] || fail 'managed processes remain; files have not been removed'
    if service_active; then fail 'service remains active; files have not been removed'; fi
    printf '%s\n' 'Zero stopped; service autostart disabled. Configuration and queues retained.'
}

check_path() {
    cleanup_cursor=$1
    while [ "$cleanup_cursor" != / ] && [ "$cleanup_cursor" != "$cleanup_root" ]; do
        if [ -L "$cleanup_cursor" ]; then
            # A masked systemd unit is the only allowed root symlink.
            if [ "$cleanup_cursor" != "$cleanup_root/etc/systemd/system/zero.service" ] || [ "$(readlink "$cleanup_cursor")" != /dev/null ]; then
                fail "refusing symlink in managed cleanup path: $cleanup_cursor"
            fi
        fi
        cleanup_cursor=$(dirname "$cleanup_cursor")
    done
}
uninstall_zero() {
    set -- "$cleanup_root/etc/systemd/system/zero.service" "$cleanup_root/etc/systemd/system/zero.service.d" \
        "$cleanup_root$cleanup_binary" "$cleanup_root/etc/zerodenet" "$cleanup_root/var/lib/zerodenet" "$cleanup_root/run/zerodenet"
    if [ "$cleanup_certificates" = yes ]; then
        set -- "$@" "$cleanup_root/etc/zboard/certificates"
        for cleanup_section in live archive renewal; do
            cleanup_base="$cleanup_root/etc/letsencrypt/$cleanup_section"
            check_path "$cleanup_base"
            for cleanup_target in "$cleanup_base"/zboard-*; do
                [ -e "$cleanup_target" ] || [ -L "$cleanup_target" ] || continue
                cleanup_name=${cleanup_target##*/}
                cleanup_number=${cleanup_name#zboard-}
                if [ "$cleanup_section" = renewal ]; then
                    case "$cleanup_number" in *.conf) cleanup_number=${cleanup_number%.conf};; *) continue;; esac
                fi
                case "$cleanup_number" in ''|*[!0-9]*) continue;; esac
                set -- "$@" "$cleanup_target"
            done
        done
    fi
    # Validate every target before stopping or removing anything.
    for cleanup_target in "$@"; do check_path "$cleanup_target"; done
    stop_zero
    for cleanup_target in "$@"; do
        check_path "$cleanup_target"
        rm -rf -- "$cleanup_target" || fail "could not remove $cleanup_target"
    done
    ctl daemon-reload || fail 'files removed, but daemon-reload failed'
    printf '%s\n' 'Managed Zero files removed. The cleanup utility is retained for reuse.'
    if [ "$cleanup_certificates" = yes ]; then
        printf '%s\n' 'ZBoard certificate files removed locally; CA revocation and provider DNS were not changed.'
    fi
    self_removal_help
}
show_status() {
    cleanup_state=$(ctl show "$cleanup_unit" --property=ActiveState --value) || fail 'cannot query systemd'
    printf 'Service: %s\nManaged PIDs:\n' "$cleanup_state"
    managed_pids
    for cleanup_target in /etc/systemd/system/zero.service /etc/systemd/system/zero.service.d \
        "$cleanup_binary" /etc/zerodenet /var/lib/zerodenet /run/zerodenet; do
        if [ -e "$cleanup_root$cleanup_target" ] || [ -L "$cleanup_root$cleanup_target" ]; then printf 'Present: %s\n' "$cleanup_target"; fi
    done
}
main() {
    cleanup_action=status
    cleanup_yes=no
    cleanup_certificates=no
    cleanup_action_set=no
    for cleanup_arg in "$@"; do
        case "$cleanup_arg" in
            status|stop|uninstall)
                [ "$cleanup_action_set" = no ] || fail 'specify only one action'
                cleanup_action=$cleanup_arg; cleanup_action_set=yes;;
            --yes) cleanup_yes=yes;;
            --certificates) cleanup_certificates=yes;;
            --help|-h)
                printf '%s\n' 'Usage: sh cleanup-zero-node.sh [status|stop|uninstall] [--yes] [--certificates]'
                self_removal_help
                return 0;;
            *) fail "unknown argument: $cleanup_arg";;
        esac
    done
    [ "$cleanup_certificates" = no ] || [ "$cleanup_action" = uninstall ] || fail '--certificates requires uninstall'
    if [ "$cleanup_action" != status ]; then
        [ "$cleanup_yes" = yes ] && [ "$(id -u)" = 0 ] || fail 'stop/uninstall requires root and --yes'
    fi
    for cleanup_tool in systemctl timeout readlink tr grep dirname rm id sleep; do
        command -v "$cleanup_tool" >/dev/null 2>&1 || fail "required system utility is missing: $cleanup_tool"
    done
    case "$cleanup_action" in status) show_status;; stop) stop_zero;; uninstall) uninstall_zero;; esac
}

main "$@"
