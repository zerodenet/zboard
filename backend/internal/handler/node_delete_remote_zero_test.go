package handler

import (
	"strings"
	"testing"
)

func TestBuildZeroNodeDeleteStopScriptDisablesAndStopsWithoutUninstalling(t *testing.T) {
	script := buildZeroNodeDeleteStopScript()
	for _, required := range []string{
		"systemctl disable --now zero.service",
		"systemctl is-active --quiet zero.service",
		"ZBOARD_ZERO_STOPPED=1",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("node delete Zero stop script missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"rm -f /usr/local/bin/zero",
		"rm -rf /etc/zerodenet",
		"apt remove",
		"dnf remove",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("node delete must retain the installed Zero artifacts; found %q", forbidden)
		}
	}
}
