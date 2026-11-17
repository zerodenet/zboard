package zero

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// The panel owns this identity. Connector remains a generic delivery service;
// the receiver and source together distinguish installations across panels.
func kernelEventStateOwner(raw []byte) (string, error) {
	var config struct {
		API struct {
			OutboxPath string `json:"outbox_path"`
			Sinks      []struct {
				Tag, Type, URL string
				SourceID       string `json:"source_id"`
			} `json:"event_sinks"`
		} `json:"api"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return "", errors.New("invalid Zero event-state configuration")
	}
	for _, sink := range config.API.Sinks {
		if sink.Tag != "zboard" || sink.Type != "webhook" {
			continue
		}
		if sink.URL == "" || sink.SourceID == "" || config.API.OutboxPath != "/var/lib/zerodenet/event-outbox.jsonl" {
			return "", errors.New("unsupported managed Zero event-state identity or path")
		}
		identity, _ := json.Marshal([]string{sink.URL, sink.SourceID})
		digest := sha256.Sum256(identity)
		return hex.EncodeToString(digest[:]), nil
	}
	return "", nil
}

func prepareKernelEventState(session KernelRemoteSession, stage string, request KernelInstallRequest) (bool, error) {
	owner, err := kernelEventStateOwner(request.RuntimeConfig)
	if err != nil || owner == "" {
		return true, err
	}
	legacy := "0"
	if request.PreserveLegacyEventState {
		legacy = "1"
	}
	command := "set -eu\n" + kernelEventStateShell + "\nevent_owner=" + shellQuote(owner) + "\nevent_legacy=" + legacy + "\nevent_state_mode\n"
	output, err := session.Run(command, true)
	if err != nil {
		return false, errors.New("cannot verify Zero event-state ownership; kernel left unchanged")
	}
	mode := strings.TrimSpace(output)
	if mode != "preserve" && mode != "reset" {
		return false, errors.New("invalid Zero event-state ownership response; kernel left unchanged")
	}
	plan := fmt.Sprintf("%s %s %s\n", owner, legacy, mode)
	if err := session.Upload(stage+"/event-state.plan", "0600", []byte(plan)); err != nil {
		return false, fmt.Errorf("stage Zero event-state ownership: %w", err)
	}
	return mode == "preserve", nil
}

// Publishing a config must not reassign an old queue before initialization has
// established its owner. Legacy, already observed nodes may keep publishing.
func stageKernelEventStateGuard(session KernelRemoteSession, stage string, raw []byte, required bool) error {
	owner, err := kernelEventStateOwner(raw)
	if err != nil || owner == "" {
		return err
	}
	legacy := "1"
	if required {
		legacy = "0"
	}
	return session.Upload(stage+"/event-state.guard", "0600", []byte(owner+" "+legacy+"\n"))
}

// Queues with another/unknown owner are moved only after the managed writers
// stop. Keep them outside the live path for rollback and accounting recovery.
// Ordinary upgrades never rewrite pending event contents.
const kernelEventStateOwnershipShell = `
event_outbox=/var/lib/zerodenet/event-outbox.jsonl
event_owner_file=/var/lib/zerodenet/zboard-event-state.owner
check_event_state_paths() {
  for path in /var /var/lib /var/lib/zerodenet "$event_outbox" "$event_owner_file" "$event_owner_file.next"; do
    test ! -L "$path" || { printf 'Refusing symlink in Zero event-state path\n' >&2; return 1; }
  done
  for path in "$event_outbox" "$event_owner_file"; do
    if [ -e "$path" ]; then test -f "$path" || return 1; fi
  done
}
event_state_mode() {
  check_event_state_paths || return 1
  if [ -f "$event_owner_file" ]; then
    if [ "$(cat "$event_owner_file")" = "$event_owner" ]; then printf 'preserve\n'; else printf 'reset\n'; fi
  elif [ "$event_legacy" = 1 ]; then
    printf 'preserve\n'
  else
    printf 'reset\n'
  fi
}
`

const kernelEventStateGuardShell = kernelEventStateOwnershipShell + `
if [ -f "$stage/event-state.guard" ]; then
  read -r event_owner event_legacy < "$stage/event-state.guard"
  if [ "$(event_state_mode)" != preserve ]; then
    printf 'ZBOARD_EVENT_STATE_INITIALIZATION_REQUIRED: initialize this node before publishing configuration\n' >&2
    exit 1
  fi
fi
`

const kernelEventStateShell = kernelEventStateOwnershipShell + `
validate_event_state_plan() {
  [ -f "$stage/event-state.plan" ] || return 0
  read -r event_owner event_legacy event_mode < "$stage/event-state.plan"
  test "$(event_state_mode)" = "$event_mode" || { printf 'Zero event-state ownership changed; retry initialization\n' >&2; return 1; }
  test ! -e "$backup/event-state.mode"
}
prepare_event_state() {
  [ -f "$stage/event-state.plan" ] || return 0
  if [ -f "$event_owner_file" ]; then cp -p "$event_owner_file" "$backup/event-state.owner"; fi
  if [ -f "$event_outbox" ]; then printf '1\n' > "$backup/event-state.had-outbox"; else printf '0\n' > "$backup/event-state.had-outbox"; fi
  event_state_touched=1
  printf '%s\n' "$event_mode" > "$backup/event-state.mode"
  if [ "$event_mode" = reset ]; then
    # This also stops manually started processes using the managed binary/config.
    sh "$stage/cleanup-zero-node.sh" stop --yes
    check_event_state_paths
    if [ -f "$event_outbox" ]; then
      mv "$event_outbox" "$backup/event-state.outbox"
    fi
    printf 'ZBOARD_EVENT_STATE_ISOLATED=1\n'
  fi
  printf '%s\n' "$event_owner" > "$event_owner_file.next"
  chmod 0600 "$event_owner_file.next"
  mv -f "$event_owner_file.next" "$event_owner_file"
}
restore_event_state() {
  [ -f "$backup/event-state.mode" ] || return 0
  [ ! -f "$backup/event-state.restored" ] || return 0
  check_event_state_paths
  if [ "$(cat "$backup/event-state.mode")" = reset ]; then
    systemctl stop zero
    # Save events emitted by the failed new installation separately.
    if [ -f "$backup/event-state.outbox" ] || [ "$(cat "$backup/event-state.had-outbox")" = 0 ]; then
      if [ -f "$event_outbox" ]; then mv "$event_outbox" "$backup/event-state.failed"; fi
      if [ -f "$backup/event-state.outbox" ]; then mv "$backup/event-state.outbox" "$event_outbox"; fi
    fi
  fi
  if [ -f "$backup/event-state.owner" ]; then
    cp -p "$backup/event-state.owner" "$event_owner_file"
  else
    rm -f "$event_owner_file"
  fi
  touch "$backup/event-state.restored"
}
`
