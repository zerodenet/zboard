package handler

import (
	"errors"
	"fmt"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// stopManagedZeroBeforeNodeDelete keeps the installed Zero files intact, but
// makes the managed systemd service unable to keep reporting after Zboard
// removes the node identity and its Connector credential.
func (h *handlers) stopManagedZeroBeforeNodeDelete(node model.Node) (bool, error) {
	knownInstalled := false
	var state model.NodeKernelState
	if err := h.db.Where("node_id = ?", node.ID).First(&state).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return false, err
		}
	} else {
		knownInstalled = strings.TrimSpace(state.InstalledVersion) != ""
	}
	// Connector history is also evidence that a Zero process has been managed
	// on this asset. This covers legacy/stale rows where kernel_state is absent.
	if !knownInstalled && node.ConnectorLastSeenAt == nil {
		return false, nil
	}
	if err := h.validateNodeSSH(node); err != nil {
		return false, fmt.Errorf("managed Zero must be stopped but SSH is unavailable: %w", err)
	}
	conn, _, err := h.dialNodeSSH(node)
	if err != nil {
		return false, fmt.Errorf("connect to stop Zero before node deletion: %w", err)
	}
	defer conn.Close()
	output, err := h.runNodeSSHSession(conn, node, buildZeroNodeDeleteStopScript(), true)
	if err != nil {
		return false, fmt.Errorf("stop Zero before node deletion: %w: %s", err, truncateKernelError(output))
	}
	return true, nil
}

func buildZeroNodeDeleteStopScript() string {
	return `set -eu
test "$(id -u)" = "0"
command -v systemctl >/dev/null
if systemctl cat zero.service >/dev/null 2>&1; then
  systemctl disable --now zero.service >/dev/null
fi
if systemctl is-active --quiet zero.service; then
  echo "Zero service is still active after disable --now" >&2
  exit 1
fi
printf 'ZBOARD_ZERO_STOPPED=1\n'
`
}
