package handler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

const (
	zeroActivationVerificationTimeout  = 45 * time.Second
	zeroActivationVerificationInterval = time.Second
	zeroActivationHealthySamples       = 3
)

func kernelActivationProbeHealthy(probe kernelProbe, expectedBinarySHA string) bool {
	return probe.Installed &&
		probe.BinarySHA256 == expectedBinarySHA &&
		probe.ServiceStatus == "active" &&
		probe.ControlStatus == "healthy"
}

func (h *handlers) verifyNodeKernelStable(parent context.Context, node model.Node, expectedBinarySHA string) (kernelProbe, error) {
	ctx, cancel := context.WithTimeout(parent, zeroActivationVerificationTimeout)
	defer cancel()

	consecutiveHealthy := 0
	var last kernelProbe
	var lastProbeErr error

	for {
		probe, err := h.probeNodeKernel(node)
		if err != nil {
			lastProbeErr = err
			consecutiveHealthy = 0
		} else {
			last = probe
			lastProbeErr = nil
			if kernelActivationProbeHealthy(probe, expectedBinarySHA) {
				consecutiveHealthy++
				if consecutiveHealthy >= zeroActivationHealthySamples {
					return probe, nil
				}
			} else {
				consecutiveHealthy = 0
			}
		}

		select {
		case <-ctx.Done():
			summary := h.captureZeroActivationFailure(node)
			if lastProbeErr != nil {
				return last, fmt.Errorf("Zero did not become stable within %s (last probe error: %v)%s", zeroActivationVerificationTimeout, lastProbeErr, formatZeroActivationSummary(summary))
			}
			return last, fmt.Errorf(
				"Zero did not become stable within %s (installed=%t sha_match=%t service=%s control=%s)%s",
				zeroActivationVerificationTimeout,
				last.Installed,
				last.BinarySHA256 == expectedBinarySHA,
				last.ServiceStatus,
				last.ControlStatus,
				formatZeroActivationSummary(summary),
			)
		case <-time.After(zeroActivationVerificationInterval):
		}
	}
}

func formatZeroActivationSummary(summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ""
	}
	return "; " + summary
}

func (h *handlers) captureZeroActivationFailure(node model.Node) string {
	const command = `set +e
printf 'zero systemd state:\n'
systemctl show zero --no-pager -p ActiveState -p SubState -p Result -p ExecMainCode -p ExecMainStatus -p NRestarts 2>/dev/null || true
printf 'recent zero journal:\n'
journalctl -u zero --no-pager -n 20 -o cat 2>/dev/null || true
exit 0`
	output, _, err := h.execSSHCommandWithPrivilege(node, command, true)
	output = strings.TrimSpace(output)
	if output != "" {
		return "Zero activation summary: " + truncateKernelError(output)
	}
	if err != nil {
		return "Zero activation summary unavailable: " + truncateKernelError(err.Error())
	}
	return ""
}
