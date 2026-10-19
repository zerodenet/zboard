package zero

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

// Execute the actual reconciliation shell. A healthy status or an old receipt
// cannot release an unfinished transaction. Repeated checks stay read-only.
func TestConfigurationReconciliationRequiresMatchingCompletedReceipt(t *testing.T) {
	sha := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, pending, receipt, core string
		applied, safe                bool
	}{
		{name: "lost SSH reply after commit", receipt: sha + " " + sha + " core 2\n", core: "core", applied: true},
		{name: "healthy process still applying", pending: "core 1 " + sha + "\n", core: "core"},
		{name: "old receipt cannot confirm new attempt", pending: "core 2 " + sha + "\n", receipt: sha + " " + sha + " core 2\n", core: "core"},
		{name: "late acknowledged transaction", pending: "core 1 " + sha + "\n", receipt: sha + " " + sha + " core 2\n", core: "core", applied: true},
		{name: "different persisted file", receipt: sha + " " + strings.Repeat("b", 64) + " core 2\n", pending: "core 1 " + sha + "\n", core: "core"},
		{name: "old core terminated", pending: "old-core 1 " + sha + "\n", core: "new-core", safe: true},
		{name: "legacy unknown without a guard", core: "core"},
		{name: "staging never submitted", pending: "core 1 " + sha + " prepared\n", core: "core", safe: true},
		{name: "malformed guard retained", pending: "bad guard\n", core: "core"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			bin := filepath.Join(root, "bin")
			state := filepath.Join(root, "state")
			etc := filepath.Join(root, "etc")
			for _, dir := range []string{bin, state, etc} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			files := map[string]string{
				filepath.Join(bin, "flock"):        "#!/bin/sh\nexit 0\n",
				filepath.Join(bin, "timeout"):      "#!/bin/sh\nshift\nexec \"$@\"\n",
				filepath.Join(bin, "sha256sum"):    "#!/bin/sh\necho '" + sha + " file'\n",
				filepath.Join(bin, "zero"):         "#!/bin/sh\n[ \"$1\" = status ] || { echo UNEXPECTED_MUTATION; exit 9; }\nprintf '{\\n  \"runtime\": {\\n    \"core_instance_id\": \"" + tc.core + "\",\\n    \"config_revision\": 2\\n  }\\n}\\n'\n",
				filepath.Join(etc, "current.json"): "{}",
			}
			if tc.pending != "" {
				files[filepath.Join(state, "zboard-config.pending")] = tc.pending
			}
			if tc.receipt != "" {
				files[filepath.Join(state, "zboard-config.receipt")] = tc.receipt
			}
			for path, data := range files {
				if err := os.WriteFile(path, []byte(data), 0700); err != nil {
					t.Fatal(err)
				}
			}
			replace := strings.NewReplacer("/usr/local/bin/zero", filepath.Join(bin, "zero"), "/etc/zerodenet", etc, "/var/lib/zerodenet", state)
			script := replace.Replace(buildConfigurationReconciliationScript(sha))
			for pass := 0; pass < 2; pass++ {
				cmd := exec.Command("bash")
				cmd.Stdin = strings.NewReader(script)
				cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
				out, err := cmd.CombinedOutput()
				output := string(out)
				if (err == nil) != (tc.applied || tc.safe) || strings.Contains(output, "ZBOARD_CONFIG_APPLIED="+sha) != tc.applied || strings.Contains(output, "ZBOARD_CONFIG_RETRY_SAFE=1") != tc.safe || strings.Contains(output, "UNEXPECTED_MUTATION") {
					t.Fatalf("err=%v output=%s", err, out)
				}
			}
			if tc.name == "healthy process still applying" {
				// Even a direct/manual publisher must respect the durable
				// guard instead of issuing another config.apply.
				stage := filepath.Join(root, "stage")
				if err := os.MkdirAll(stage, 0700); err != nil {
					t.Fatal(err)
				}
				for path, body := range map[string]string{
					filepath.Join(bin, "id"):             "#!/bin/sh\necho 0\n",
					filepath.Join(bin, "systemctl"):      "#!/bin/sh\n[ \"$1\" = is-active ] || { echo UNEXPECTED_MUTATION; exit 9; }\n",
					filepath.Join(stage, "runtime.json"): "{}",
				} {
					if err := os.WriteFile(path, []byte(body), 0700); err != nil {
						t.Fatal(err)
					}
				}
				cmd := exec.Command("bash")
				cmd.Stdin = strings.NewReader(replace.Replace(BuildConfigurationPublishScript(stage, sha, 9)))
				cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
				output, err := cmd.CombinedOutput()
				if err == nil || !strings.Contains(string(output), "ZBOARD_CONFIG_OUTCOME_UNKNOWN=1") || strings.Contains(string(output), "UNEXPECTED_MUTATION") {
					t.Fatalf("guard bypassed: %v %s", err, output)
				}
			}

			if tc.pending != "" {
				raw, err := os.ReadFile(filepath.Join(state, "zboard-config.pending"))
				if err != nil || string(raw) != tc.pending {
					t.Fatal("reconciliation mutated the pending guard")
				}
			}
		})
	}
}

func TestUnconfirmedApplyIsTypedAndRetainsCancellation(t *testing.T) {
	session := &kernelRemoteSessionStub{runErrorAt: 3, runErrorOutput: "ZBOARD_CONFIG_PHASE=reload\nTerminated"}
	result, err := (ConfigurationPublisher{Dialer: &kernelRemoteDialerStub{session: session}}).Publish(context.Background(), ConfigurationPublishRequest{DeploymentID: 1, RuntimeConfig: []byte("{}"), ConfigSHA256: strings.Repeat("a", 64), ConnectorKey: "test"})
	if !errors.Is(err, network.ErrConfigurationOutcomeUnknown) || !result.MayHaveApplied {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if configurationConfirmedUnapplied("ZBOARD_CONFIG_PHASE=reload\nZBOARD_CONFIG_CONTROL_UNAVAILABLE") {
		t.Fatal("post-apply probe failure was treated as unapplied")
	}
}
