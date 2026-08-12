package handler

import (
	"strings"
	"testing"
)

func TestParseNodeBBRState(t *testing.T) {
	state, err := parseNodeBBRState(strings.Join([]string{
		"ZBOARD_BBR_AVAILABLE=reno cubic bbr",
		"ZBOARD_BBR_CURRENT=bbr",
		"ZBOARD_BBR_QDISC=fq",
		"ZBOARD_BBR_PERSISTENT=1",
		"ZBOARD_BBR_KERNEL=6.8.0-test",
	}, "\n"))
	if err != nil {
		t.Fatalf("parse BBR state: %v", err)
	}
	if !state.Available || !state.Active || !state.Persistent {
		t.Fatalf("unexpected BBR state: %+v", state)
	}
	if state.DefaultQdisc != "fq" || state.KernelRelease != "6.8.0-test" {
		t.Fatalf("unexpected BBR details: %+v", state)
	}
}

func TestParseNodeBBRStateUnavailable(t *testing.T) {
	state, err := parseNodeBBRState(strings.Join([]string{
		"ZBOARD_BBR_AVAILABLE=reno cubic",
		"ZBOARD_BBR_CURRENT=cubic",
		"ZBOARD_BBR_QDISC=fq_codel",
		"ZBOARD_BBR_PERSISTENT=0",
		"ZBOARD_BBR_KERNEL=5.4.0-test",
	}, "\n"))
	if err != nil {
		t.Fatalf("parse BBR state: %v", err)
	}
	if state.Available || state.Active || state.Persistent {
		t.Fatalf("BBR should remain unavailable/inactive: %+v", state)
	}
}

func TestNodeBBREnableCommandUsesOwnedSysctlDropIn(t *testing.T) {
	for _, required := range []string{
		"/etc/sysctl.d/99-zboard-bbr.conf",
		"net.core.default_qdisc = fq",
		"net.ipv4.tcp_congestion_control = bbr",
		"ZBOARD_BBR_UNAVAILABLE=1",
		"rollback()",
	} {
		if !strings.Contains(nodeBBREnableCommand, required) {
			t.Fatalf("BBR action is missing safety contract %q", required)
		}
	}
	if strings.Contains(nodeBBREnableCommand, "sysctl --system") {
		t.Fatal("BBR action must not reload unrelated sysctl configuration")
	}
	if strings.Contains(nodeBBREnableCommand, "curl ") || strings.Contains(nodeBBREnableCommand, "wget ") {
		t.Fatal("BBR action must not execute third-party bootstrap scripts")
	}
}

func TestNodeBBRProbeRejectsMissingSysctl(t *testing.T) {
	if _, err := parseNodeBBRState("ZBOARD_BBR_ERROR=sysctl_not_found\n"); err == nil {
		t.Fatal("missing sysctl must be reported as unsupported")
	}
}
