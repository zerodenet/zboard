package handler

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestCertificateDeleteScriptRejectsUnmanagedPaths(t *testing.T) {
	if _, err := buildCertificateDeleteScript(model.ManagedCertificate{ID: 1, CertPath: "/etc/other/cert.pem"}); err == nil {
		t.Fatal("unmanaged path accepted")
	}
	if _, err := buildCertificateDeleteScript(model.ManagedCertificate{}); err == nil {
		t.Fatal("zero ID accepted")
	}
}

func TestCertificateDeleteScriptExecutesBoundedRetryableCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX execution runs in Linux CI")
	}
	if _, err := exec.LookPath("flock"); err != nil {
		t.Skip("flock unavailable")
	}
	for _, scenario := range []string{"success", "ca_failure", "already_revoked", "symlink", "foreign_lineage"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			write := func(name, content string, mode os.FileMode) {
				t.Helper()
				p := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(content), mode); err != nil {
					t.Fatal(err)
				}
			}
			write("bin/id", "#!/bin/sh\necho 0\n", 0700)
			write("bin/openssl", "#!/bin/sh\nexit 0\n", 0700)
			write("bin/certbot", `#!/bin/sh
set -eu
echo "$1" >> "$FIXTURE/calls"
case "$1" in
revoke)
  if [ "$SCENARIO" = ca_failure ]; then echo 'connection failed'; exit 1; fi
  if [ "$SCENARIO" = already_revoked ]; then echo 'urn:ietf:params:acme:error:alreadyRevoked'; exit 1; fi
  ;;
delete)
  test "$4" = zboard-7
  rm -rf "$FIXTURE/letsencrypt/live/zboard-7" "$FIXTURE/letsencrypt/archive/zboard-7"
  rm -f "$FIXTURE/letsencrypt/renewal/zboard-7.conf"
  ;;
esac
`, 0700)
			write("letsencrypt/archive/zboard-7/cert1.pem", "certificate", 0600)
			write("letsencrypt/archive/zboard-7/privkey1.pem", "private fixture", 0600)
			write("letsencrypt/live/zboard-7/present", "fixture", 0600)
			write("letsencrypt/renewal/zboard-7.conf", "archive_dir = "+root+"/letsencrypt/archive/zboard-7\n", 0600)
			write("zboard/certificates/7/generations/abc/fullchain.pem", "certificate", 0600)
			write("zboard/certificates/7/generations/abc/privkey.pem", "private fixture", 0600)
			write("zboard/certificates/8/keep", "unrelated", 0600)
			if err := os.MkdirAll(filepath.Join(root, "locks"), 0700); err != nil {
				t.Fatal(err)
			}
			if scenario == "symlink" {
				if err := os.RemoveAll(filepath.Join(root, "zboard/certificates/7")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(root, "zboard/certificates/8"), filepath.Join(root, "zboard/certificates/7")); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "foreign_lineage" {
				write("letsencrypt/renewal/zboard-7.conf", "archive_dir = /unrelated/archive\n", 0600)
			}
			script, err := buildCertificateDeleteScript(model.ManagedCertificate{ID: 7, Environment: certificateEnvironmentStaging})
			if err != nil {
				t.Fatal(err)
			}
			script = strings.NewReplacer("/etc/zboard", root+"/zboard", "/etc/letsencrypt", root+"/letsencrypt", "/run/lock", root+"/locks").Replace(script)
			run := func() ([]byte, error) {
				cmd := exec.Command("sh", "-c", script)
				cmd.Env = append(os.Environ(), "FIXTURE="+root, "SCENARIO="+scenario, "PATH="+root+"/bin:"+os.Getenv("PATH"))
				return cmd.CombinedOutput()
			}
			output, err := run()
			wantFailure := scenario == "ca_failure" || scenario == "symlink" || scenario == "foreign_lineage"
			if wantFailure {
				if err == nil {
					t.Fatalf("unsafe success: %s", output)
				}
				if _, err := os.Stat(filepath.Join(root, "letsencrypt/archive/zboard-7/cert1.pem")); err != nil {
					t.Fatal("failure lost material")
				}
			} else {
				if err != nil {
					t.Fatalf("cleanup: %v %s", err, output)
				}
				for _, p := range []string{"zboard/certificates/7", "letsencrypt/archive/zboard-7", "letsencrypt/live/zboard-7", "letsencrypt/renewal/zboard-7.conf"} {
					if _, err := os.Stat(filepath.Join(root, p)); !os.IsNotExist(err) {
						t.Fatalf("retained %s", p)
					}
				}
				if output, err := run(); err != nil {
					t.Fatalf("retry of completed deletion: %v %s", err, output)
				}
			}
			if data, err := os.ReadFile(filepath.Join(root, "zboard/certificates/8/keep")); err != nil || string(data) != "unrelated" {
				t.Fatal("unrelated resource changed")
			}
		})
	}
}
