package handler

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

func init() {
	// Diagnostic logs are operator-visible, so redact common structured and
	// header forms in addition to plain key=value output. In particular,
	// consume the complete Bearer credential instead of redacting only the
	// scheme token.
	nodeDiagnosticSecretPattern = regexp.MustCompile(`(?i)(["']?(password|token|secret|api[_-]?key|authorization|credential)["']?\s*[:=]\s*)(("[^"]*")|('[^']*')|(bearer\s+[^\s,;]+)|([^\s,;]+))`)
}

// listenerAddressSatisfiesConfiguredBind is deliberately stricter than the
// legacy compatibility matcher: a configured loopback/specific address is not
// satisfied by a wildcard socket. A wider bind means the expected listener is
// not present exactly as configured and should be surfaced to the operator.
func listenerAddressSatisfiesConfiguredBind(expected, actual string) bool {
	expected = normalizeListenerAddress(expected)
	actual = normalizeListenerAddress(actual)
	if expected == actual {
		return true
	}

	expectedIP := net.ParseIP(expected)
	actualIP := net.ParseIP(actual)
	if expectedIP == nil {
		if expected == "*" {
			return actual == "*" || (actualIP != nil && actualIP.IsUnspecified())
		}
		return false
	}
	if !expectedIP.IsUnspecified() {
		return actualIP != nil && expectedIP.Equal(actualIP)
	}
	if actual == "*" {
		return true
	}
	return actualIP != nil && actualIP.IsUnspecified() && ((expectedIP.To4() == nil) == (actualIP.To4() == nil))
}

func appendNodeDiagnosticBindingWarnings(snapshot *nodeDiagnosticSnapshot) {
	for _, expected := range snapshot.ExpectedListeners {
		if expected.Present || len(expected.MissingNetworks) == 0 {
			continue
		}
		for _, network := range expected.MissingNetworks {
			for _, actual := range snapshot.ActualListeners {
				if actual.Network != network || actual.Port != expected.Port {
					continue
				}
				if !listenerAddressMatches(expected.Address, actual.Address) || listenerAddressSatisfiesConfiguredBind(expected.Address, actual.Address) {
					continue
				}
				snapshot.Warnings = append(snapshot.Warnings, fmt.Sprintf(
					"%s 监听端口 %d 实际绑定 %s，与 Zero 配置期望 %s 不一致；监听范围扩大不会按正常监听处理。",
					strings.ToUpper(network), expected.Port, actual.Address, expected.Address,
				))
				break
			}
		}
	}
}
