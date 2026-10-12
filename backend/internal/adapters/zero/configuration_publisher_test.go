package zero

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Execute the generated remote script against local command doubles. No node
// or system service is contacted, and every absolute data path is isolated.
func TestMatchingActiveConfigurationNeverRestartsOnControlProbeFailure(t *testing.T) {
	for _, healthy := range []bool{true, false} {
		t.Run(fmt.Sprint(healthy), func(t *testing.T) {
			root := t.TempDir()
			stage := filepath.Join(root, "stage")
			bin := filepath.Join(root, "bin")
			config := filepath.Join(root, "etc")
			state := filepath.Join(root, "var")
			for _, dir := range []string{stage, bin, config, state} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			sha := strings.Repeat("a", 64)
			mutations := filepath.Join(root, "service-mutations")
			status := "exit 1"
			if healthy {
				status = "printf '{\\n  \"runtime\": {\\n    \"core_instance_id\": \"test-core\",\\n    \"config_revision\": 1\\n  }\\n}\\n'"
			}
			files := map[string]string{
				filepath.Join(stage, "runtime.json"):          "{}",
				filepath.Join(stage, "zero.env"):              "ZERO_PANEL_API_KEY=test\n",
				filepath.Join(config, "current.json"):         "{}",
				filepath.Join(config, "zero.env"):             "ZERO_PANEL_API_KEY=test\n",
				filepath.Join(bin, "flock"):                   "#!/bin/sh\nexit 0\n",
				filepath.Join(state, "zboard-config.receipt"): sha + " " + sha + " test-core 1\n",
				filepath.Join(bin, "id"):                      "#!/bin/sh\necho 0\n",
				filepath.Join(bin, "sha256sum"):               "#!/bin/sh\necho '" + sha + "  file'\n",
				filepath.Join(bin, "systemctl"):               "#!/bin/sh\nif [ \"$1\" = is-active ]; then exit 0; fi\necho \"$*\" >> " + shellQuote(mutations) + "\necho UNEXPECTED_SERVICE_MUTATION\nexit 1\n",
				filepath.Join(bin, "zero"):                    "#!/bin/sh\nif [ \"$1\" = validate ]; then exit 0; fi\n" + status + "\n",
			}
			for path, body := range files {
				if err := os.WriteFile(path, []byte(body), 0700); err != nil {
					t.Fatal(err)
				}
			}
			script := strings.NewReplacer("/usr/local/bin/zero", filepath.Join(bin, "zero"), "/etc/zerodenet", config, "/var/lib/zerodenet", filepath.Join(root, "var"), controlSocket, filepath.Join(root, "control.sock")).Replace(BuildConfigurationPublishScript(stage, sha, 8))
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, "bash")
			command.Stdin = strings.NewReader(script)
			command.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
			output, err := command.CombinedOutput()
			if _, statErr := os.Stat(mutations); statErr == nil {
				t.Fatalf("control probe caused restart: %s", output)
			}
			if healthy {
				if err != nil || !strings.Contains(string(output), "ZBOARD_CONFIG_UNCHANGED="+sha) {
					t.Fatalf("healthy config: %s %v", output, err)
				}
			} else if err == nil || !strings.Contains(string(output), "ZBOARD_CONFIG_CONTROL_UNAVAILABLE") {
				t.Fatalf("failed probe must retain publication for retry: %s %v", output, err)
			}
		})
	}
}

func TestConfigurationPublisherStagesAndActivatesThroughRemotePort(t *testing.T) {
	session := &kernelRemoteSessionStub{}
	now := time.Date(2026, time.September, 16, 4, 0, 0, 0, time.UTC)
	publisher := ConfigurationPublisher{
		Dialer: &kernelRemoteDialerStub{session: session}, NewStageID: func() string { return "config-stage" }, Now: func() time.Time { return now },
	}
	result, err := publisher.Publish(context.Background(), ConfigurationPublishRequest{
		DeploymentID: 8, RuntimeConfig: []byte(`{"inbounds":[]}`), ConfigSHA256: strings.Repeat("a", 64), ConnectorKey: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.ActivatedAt.Equal(now) || len(session.runs) != 3 || len(session.uploads) != 2 || !session.runs[2].privileged || !strings.Contains(session.runs[2].command, "ZBOARD_CONFIG_APPLIED") {
		t.Fatalf("result=%+v runs=%+v uploads=%+v", result, session.runs, session.uploads)
	}
}

func TestConfigurationPublishSkipsRestartForHealthyMatchingRuntime(t *testing.T) {
	script := BuildConfigurationPublishScript("/tmp/staged", strings.Repeat("a", 64), 8)
	check := exec.Command("bash", "-n")
	check.Stdin = strings.NewReader(script)
	if output, err := check.CombinedOutput(); err != nil {
		t.Fatalf("invalid publish script: %v %s", err, output)
	}
	for _, fragment := range []string{"runtime_identity", "zboard-config.receipt", "ZBOARD_CONFIG_UNCHANGED=%s", "/usr/local/bin/zero reload", "flock -w 20"} {
		if !strings.Contains(script, fragment) {
			t.Fatalf("publication misses %s", fragment)
		}
	}
	if strings.Contains(script, "systemctl restart") || strings.Contains(BuildConfigurationRollbackScript(8), "systemctl restart") {
		t.Fatal("normal publication or rollback can restart the kernel")
	}

}

func TestConfigurationPublisherRequiresDeclaredDirectInboundUDP(t *testing.T) {
	session := &kernelRemoteSessionStub{buildInfo: strings.ReplaceAll(nativeTestBuildInfo, `"supported":true`, `"supported":false`)}
	publisher := ConfigurationPublisher{Dialer: &kernelRemoteDialerStub{session: session}}
	_, err := publisher.Publish(context.Background(), ConfigurationPublishRequest{
		DeploymentID: 8, RuntimeConfig: []byte(`{"inbounds":[{"protocol":{"type":"direct"}}]}`), ConfigSHA256: strings.Repeat("a", 64), ConnectorKey: "secret",
	})
	if err == nil || !strings.Contains(err.Error(), "direct 入站 UDP") || len(session.uploads) != 0 {
		t.Fatalf("runs=%+v uploads=%+v err=%v", session.runs, session.uploads, err)
	}
}

func TestConfigurationPublisherAdaptsByCapabilitiesInsteadOfVersion(t *testing.T) {
	for _, test := range []struct {
		name, info string
		applies    bool
	}{
		{"older dev with required capabilities", strings.Replace(nativeTestBuildInfo, NativeBaselineVersion, "0.0.3-dev.202610100641", 1), true},
		{"other version with required capabilities", strings.Replace(nativeTestBuildInfo, NativeBaselineVersion, "0.0.15", 1), true},
		{"no hot apply keeps existing state", strings.Replace(nativeTestBuildInfo, "status-api,connector", "connector", 1), false},
		{"missing configured protocol keeps existing state", strings.Replace(nativeTestBuildInfo, `"protocol":"vless","compiled":true`, `"protocol":"vless","compiled":false`, 1), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := &kernelRemoteSessionStub{buildInfo: test.info}
			result, err := (ConfigurationPublisher{Dialer: &kernelRemoteDialerStub{session: session}}).Publish(context.Background(), ConfigurationPublishRequest{DeploymentID: 8, RuntimeConfig: []byte(`{"inbounds":[{"protocol":{"type":"vless"}}]}`), ConfigSHA256: strings.Repeat("a", 64), ConnectorKey: "secret"})
			if test.applies {
				if err != nil || len(session.uploads) != 2 {
					t.Fatalf("available capability not used: %v", err)
				}
			} else if !errors.Is(err, ErrConfigurationCapabilityUnavailable) || len(session.uploads) != 0 || len(session.runs) != 1 || result.MayHaveApplied {
				t.Fatalf("unsupported operation changed node: result=%+v runs=%+v uploads=%+v err=%v", result, session.runs, session.uploads, err)
			}
		})
	}
}

func TestConfigurationFailureDistinguishesUnappliedSettingsFromLostAcknowledgement(t *testing.T) {
	for _, test := range []struct {
		output    string
		unapplied bool
	}{
		{"ZBOARD_CONFIG_HOT_APPLY_UNAVAILABLE", true},
		{"ZBOARD_CONFIG_VALIDATION_REJECTED_CURRENT_UNCHANGED", true},
		{"ZBOARD_CONFIG_CONTROL_UNAVAILABLE", true},
		{"SSH connection lost", false},
		{"ZBOARD_CONFIG_COMMITTED=digest\nZBOARD_CONFIG_CONTROL_UNAVAILABLE", false},
	} {
		if got := configurationConfirmedUnapplied(test.output); got != test.unapplied {
			t.Fatalf("output=%q unapplied=%t want=%t", test.output, got, test.unapplied)
		}
	}
}

func TestNativeUnknownConfigurationFieldDefersWithoutDroppingConstraints(t *testing.T) {
	session := &kernelRemoteSessionStub{runErrorAt: 3, runErrorOutput: "unknown field `dial`\nZBOARD_CONFIG_VALIDATION_REJECTED_CURRENT_UNCHANGED"}
	config := []byte(`{"outbounds":[{"tag":"direct-v6","protocol":{"type":"direct"},"dial":{"address_family":"only_ipv6"}}]}`)
	result, err := (ConfigurationPublisher{Dialer: &kernelRemoteDialerStub{session: session}}).Publish(context.Background(), ConfigurationPublishRequest{DeploymentID: 8, RuntimeConfig: config, ConfigSHA256: strings.Repeat("a", 64), ConnectorKey: "secret"})
	if !errors.Is(err, ErrConfigurationCapabilityUnavailable) || result.MayHaveApplied || result.Committed {
		t.Fatalf("unsupported constraint was not deferred safely: %+v %v", result, err)
	}
	if len(session.uploads) != 2 || string(session.uploads[0].data) != string(config) || strings.Contains(session.runs[2].command, "systemctl restart") {
		t.Fatal("constraint altered or restart fallback introduced")
	}
}

func TestConfigurationPublisherPropagatesCanceledDial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (ConfigurationPublisher{Dialer: &kernelRemoteDialerStub{session: &kernelRemoteSessionStub{}}}).Publish(ctx, ConfigurationPublishRequest{
		DeploymentID: 8, RuntimeConfig: []byte("{}"), ConfigSHA256: strings.Repeat("a", 64), ConnectorKey: "secret",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

func TestSupportsDirectInboundUDP(t *testing.T) {
	supported := `protocol_capabilities:[{"protocol":"direct","compiled":true,"inbound":{"udp":{"supported":true}}}]`
	if !SupportsDirectInboundUDP(supported) || SupportsDirectInboundUDP(`protocol_capabilities:[{"protocol":"direct","compiled":false,"inbound":{"udp":{"supported":true}}}]`) {
		t.Fatal("direct UDP capability projection is incorrect")
	}
}
