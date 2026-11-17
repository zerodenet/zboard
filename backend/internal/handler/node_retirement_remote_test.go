package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestRetirementDNSChecksRemoteOwnershipAndSafelyRetriesAbsence(t *testing.T) {
	for _, outcome := range []string{"owned", "changed", "foreign", "missing", "denied"} {
		t.Run(outcome, func(t *testing.T) {
			f, node, account := deletionFixture(t)
			record := seedDeletionDNS(t, f, node, account, "remote")
			deleted := 0
			mockDeletionDNS(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodDelete {
					deleted++
					_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]string{"id": "remote"}})
					return
				}
				if outcome == "missing" || deleted > 0 {
					w.WriteHeader(404)
					_, _ = w.Write([]byte(`{"success":false}`))
					return
				}
				if outcome == "denied" {
					w.WriteHeader(403)
					_, _ = w.Write([]byte(`{"success":false}`))
					return
				}
				remote := cloudflareRecord{ID: record.ProviderRecordID, Type: record.RecordType, Name: record.DomainName, Content: record.RecordValue, TTL: record.TTL, Proxied: record.Proxied, Comment: fmt.Sprintf("human note | [ZBoard dns=%d node=%d]", record.ID, node.ID)}
				if outcome == "changed" {
					remote.Content = "192.0.2.200"
				}
				if outcome == "foreign" {
					remote.Comment = "managed elsewhere"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": remote})
			})
			message, err := f.h.retireDNS(t.Context(), record)
			if outcome == "owned" {
				if err != nil || message != "" || deleted != 1 {
					t.Fatal(message, err, deleted)
				}
				if _, err := f.h.retireDNS(t.Context(), record); err != nil || deleted != 1 {
					t.Fatal("retry should verify already absent", err, deleted)
				}
			} else if outcome == "denied" {
				if err == nil || deleted != 0 {
					t.Fatal("permission failure swallowed", err, deleted)
				}
			} else {
				if err != nil || deleted != 0 {
					t.Fatal(message, err, deleted)
				}
				if outcome != "missing" && message == "" {
					t.Fatal("foreign record not reported retained")
				}
			}
			var retained model.ManagedDNSRecord
			if err := f.h.db.First(&retained, record.ID).Error; err != nil {
				t.Fatal("external step removed local evidence", err)
			}
		})
	}
}

func TestRetirementCertificateCommandOnlyRemovesOwnedPaths(t *testing.T) {
	if _, err := exec.LookPath("timeout"); err != nil {
		t.Skip("requires timeout utility")
	}
	root := t.TempDir()
	managed := filepath.Join(root, "etc/zboard/certificates")
	for _, id := range []string{"7", "8"} {
		path := filepath.Join(managed, id)
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "sentinel"), []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	command, ok := retirementCertificateCommand(model.ManagedCertificate{ID: 7, CertPath: "/etc/zboard/certificates/7/current/fullchain.pem", KeyPath: "/etc/zboard/certificates/7/current/privkey.pem"})
	if !ok {
		t.Fatal("managed path rejected")
	}
	command = strings.ReplaceAll(command, "/etc/", root+"/etc/")
	for i := 0; i < 2; i++ {
		if output, err := exec.Command("sh", "-c", command).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, output)
		}
	}
	if _, err := os.Stat(filepath.Join(managed, "8/sentinel")); err != nil {
		t.Fatal("another certificate removed", err)
	}
	if _, err := os.Stat(filepath.Join(managed, "7")); !os.IsNotExist(err) {
		t.Fatal("owned certificate remains", err)
	}
	if _, ok := retirementCertificateCommand(model.ManagedCertificate{ID: 7, CertPath: "/shared/cert.pem"}); ok {
		t.Fatal("custom certificate cleanup allowed")
	}
	if err := os.Symlink(filepath.Join(managed, "8"), filepath.Join(managed, "7")); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("sh", "-c", command).Run(); err == nil {
		t.Fatal("symlink cleanup accepted")
	}
	if _, err := os.Stat(filepath.Join(managed, "8/sentinel")); err != nil {
		t.Fatal("symlink target removed", err)
	}
}

func TestRetirementHTTPReadRequiresAdminAndRejectsInvalidMode(t *testing.T) {
	f, node, _ := deletionFixture(t)
	path := fmt.Sprintf("/api/v1/admin/nodes/%d/retirement", node.ID)
	// No remote job is submitted in these authority/validation checks.
	for _, tc := range []struct {
		token, body string
		status      int
	}{{f.token, `{"mode":"detach"}`, 403}, {f.admin, `{"mode":"invalid"}`, 400}, {f.admin, `{"mode":"retire","force":true}`, 400}} {
		w := httptest.NewRecorder()
		f.h.NodeRetirementHandler(w, announcementRequest("POST", path, tc.token, tc.body))
		if w.Code != tc.status {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
}

func TestRetirementDetachCompletesThroughNativeJobAndInvalidatesEventCredential(t *testing.T) {
	f, node, _ := deletionFixture(t)
	item, err := f.h.services.NodeRetirement(f.h).Repository.Start(t.Context(), 99, node.ID, "detach")
	if err != nil {
		t.Fatal(err)
	}
	f.h.zeroEventAuthCache.Store(node.ID, zeroEventCredentialCacheEntry{node: network.EventNode{ID: node.ID}, secret: "old-token", expiresAt: time.Now().Add(time.Minute)})
	executor := jobs.Executor{Store: jobstore.New(f.h.db), Worker: "native-retirement-test", Timeout: time.Minute, Handlers: map[string]jobs.Handler{network.NodeRetirementHandler: f.h.executeNodeRetirement}}
	if err := executor.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.h.zeroEventAuthCache.Load(node.ID); ok {
		t.Fatal("deleted node retains a valid credential cache")
	}
	w := httptest.NewRecorder()
	f.h.NodeRetirementHandler(w, announcementRequest("GET", fmt.Sprintf("/api/v1/admin/nodes/%d/retirement", node.ID), f.admin, ""))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"succeeded"`) || strings.Contains(w.Body.String(), "old-token") || !strings.Contains(w.Body.String(), item.RunID) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("progress is cacheable")
	}
}
