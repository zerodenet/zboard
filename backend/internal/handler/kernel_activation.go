package handler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

const (
	// Large durable outboxes are replayed before Zero exposes its control socket.
	zeroActivationVerificationTimeout  = 3 * time.Minute
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
	return verifyKernelActivation(parent, expectedBinarySHA, zeroActivationVerificationTimeout, zeroActivationVerificationInterval,
		func(ctx context.Context) (kernelProbe, error) { return h.probeNodeKernelContext(ctx, node) },
		func() string { return h.captureZeroActivationFailure(node) })
}

func verifyKernelActivation(parent context.Context, expectedBinarySHA string, timeout, interval time.Duration,
	probeKernel func(context.Context) (kernelProbe, error), captureFailure func() string,
) (kernelProbe, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	consecutiveHealthy := 0
	var last kernelProbe
	var lastProbeErr error

	for {
		probe, err := probeKernel(ctx)
		if err != nil {
			lastProbeErr = err
			consecutiveHealthy = 0
		} else {
			last = probe
			lastProbeErr = nil
			if kernelActivationProbeHealthy(probe, expectedBinarySHA) {
				consecutiveHealthy++
				if consecutiveHealthy >= zeroActivationHealthySamples && ctx.Err() == nil {
					return probe, nil
				}
			} else {
				consecutiveHealthy = 0
			}
		}

		select {
		case <-ctx.Done():
			summary := captureFailure()
			if lastProbeErr != nil {
				return last, fmt.Errorf("Zero did not become stable within %s (last probe error: %v)%s", timeout, lastProbeErr, formatZeroActivationSummary(summary))
			}
			return last, fmt.Errorf(
				"Zero did not become stable within %s (installed=%t sha_match=%t service=%s control=%s)%s",
				timeout,
				last.Installed,
				last.BinarySHA256 == expectedBinarySHA,
				last.ServiceStatus,
				last.ControlStatus,
				formatZeroActivationSummary(summary),
			)
		case <-time.After(interval):
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const command = `set +e
printf 'zero systemd state:\n'
systemctl show zero --no-pager -p ActiveState -p SubState -p Result -p ExecMainCode -p ExecMainStatus -p NRestarts 2>/dev/null || true
printf 'recent zero journal:\n'
journalctl -u zero --no-pager -n 20 -o cat 2>/dev/null || true
exit 0`
	output, _, err := h.execSSHCommandWithPrivilegeContext(ctx, node, command, true)
	output = strings.TrimSpace(output)
	if output != "" {
		return "Zero activation summary: " + truncateKernelError(output)
	}
	if err != nil {
		return "Zero activation summary unavailable: " + truncateKernelError(err.Error())
	}
	return ""
}
