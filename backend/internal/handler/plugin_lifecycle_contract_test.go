package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/plugins"
)

func TestPluginLifecycleBrowserPayloadsEnterHTTPStateMachine(t *testing.T) {
	// The frontend tests assert these bodies against Axios's actual serialized
	// requests. Feed the exact same JSON into real HTTP handlers and a real plugin.
	raw, err := os.ReadFile("../../../frontend/src/api/testdata/pluginLifecycleRequests.json")
	if err != nil {
		t.Fatal(err)
	}
	var requests []struct {
		Action string          `json:"action"`
		Body   json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(raw, &requests); err != nil {
		t.Fatal(err)
	}
	archive, keys := configBridgePackage(t)
	f := newPluginLifecycleFixture(t, archive, keys)
	status, body := f.request(t, http.MethodPut, "/api/v1/admin/plugins/"+f.installation.ID+"/config", f.adminToken, `{"revision":0,"config":{"healthy":true,"retained":"fixture"}}`)
	if status != 200 {
		t.Fatalf("configuration setup failed: %d %s", status, body)
	}
	for _, request := range requests {
		t.Run(request.Action, func(t *testing.T) {
			before, err := f.manager.Operations(f.installation.ID)
			if err != nil {
				t.Fatal(err)
			}
			status, body := f.request(t, http.MethodPost, "/api/v1/admin/plugins/"+f.installation.ID+"/actions/"+request.Action, f.adminToken, string(request.Body))
			if status != 200 {
				t.Fatalf("browser body rejected: %d %s", status, body)
			}
			var response struct{ Data plugins.Installation }
			if err := json.Unmarshal(body, &response); err != nil {
				t.Fatal(err)
			}
			if response.Data.Enabled != (request.Action == "enable") {
				t.Fatalf("unexpected enabled state: %+v", response.Data)
			}
			after, err := f.manager.Operations(f.installation.ID)
			if err != nil || len(after) != len(before)+1 {
				t.Fatalf("operation not recorded: %v %+v", err, after)
			}
			found := false
			for _, op := range after {
				if op.Action == request.Action && op.State == "succeeded" {
					found = true
				}
			}
			if !found {
				t.Fatalf("successful operation missing: %+v", after)
			}
			var stored model.PluginInstallation
			if err := f.h.db.First(&stored, "id = ?", f.installation.ID).Error; err != nil {
				t.Fatal(err)
			}
			if request.Action == "uninstall" && stored.ConfigCiphertext == "" {
				t.Fatal("uninstall deleted retained configuration")
			}
			if request.Action == "purge_data" && stored.ConfigCiphertext != "" {
				t.Fatal("purge did not clear retained configuration")
			}
			status, body = f.request(t, http.MethodGet, "/api/v1/admin/plugins", f.adminToken, "")
			var list struct{ Data []plugins.Installation }
			if status != 200 || json.Unmarshal(body, &list) != nil || len(list.Data) != 1 || list.Data[0].Generation != response.Data.Generation || list.Data[0].Enabled != response.Data.Enabled {
				t.Fatalf("reloaded state differs: %d %s", status, body)
			}
		})
	}
	var users int64
	if err := f.h.db.Model(&model.User{}).Count(&users).Error; err != nil || users != 2 {
		t.Fatalf("purge touched core users: %d %v", users, err)
	}
}

func TestPluginLifecycleHTTPPreservesStrictDecodingAndAdmission(t *testing.T) {
	failureFile := filepath.Join(t.TempDir(), "fail-start")
	archive, keys := configBridgePackageWithStartupFailure(t, failureFile)
	cases := []struct {
		name     string
		status   int
		contains string
		alter    func(*testing.T, *pluginLifecycleFixture)
		body     string
		member   bool
		action   string
	}{
		{name: "retired field", status: 400, contains: "invalid or oversized", body: `{"generation":1,"version_id":""}`},
		{name: "unknown field", status: 400, contains: "invalid or oversized", body: `{"generation":1,"unknown":true}`},
		{name: "stale generation", status: 409, contains: "refresh", body: `{"generation":0}`},
		{name: "non administrator", status: 403, contains: "admin required", member: true},
		{name: "removed rollback", status: 400, contains: "unsupported plugin operation", action: "rollback"},
		{name: "incompatible", status: 400, contains: "incompatible", alter: func(t *testing.T, f *pluginLifecycleFixture) {
			var version model.PluginVersion
			if err := f.h.db.First(&version, "id = ?", f.installation.VersionID).Error; err != nil {
				t.Fatal(err)
			}
			var manifest plugins.Manifest
			if err := json.Unmarshal([]byte(version.Manifest), &manifest); err != nil {
				t.Fatal(err)
			}
			manifest.Requires.Protocol = 2
			data, _ := json.Marshal(manifest)
			if err := f.h.db.Model(&version).Update("manifest", string(data)).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "admission revoked", status: 403, contains: "admission scope", alter: func(t *testing.T, f *pluginLifecycleFixture) {
			if err := f.h.db.Where("plugin_id = ?", f.installation.ID).Delete(&model.PluginAuthorization{}).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "migration incompatible", status: 400, contains: "data preparation", alter: func(t *testing.T, f *pluginLifecycleFixture) {
			if err := f.h.db.Create(&model.PluginData{PluginID: f.installation.ID, Version: 1}).Error; err != nil {
				t.Fatal(err)
			}
		}},
		{name: "process startup failure", status: 400, contains: "process failed to start", alter: func(t *testing.T, f *pluginLifecycleFixture) {
			if err := os.WriteFile(failureFile, []byte("fail"), 0600); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Remove(failureFile) })
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPluginLifecycleFixture(t, archive, keys)
			if tc.alter != nil {
				tc.alter(t, f)
			}
			body := tc.body
			if body == "" {
				body = `{"generation":1,"accept_untested":false}`
			}
			action := tc.action
			if action == "" {
				action = "enable"
			}
			token := f.adminToken
			if tc.member {
				token = f.memberToken
			}
			status, response := f.request(t, http.MethodPost, "/api/v1/admin/plugins/"+f.installation.ID+"/actions/"+action, token, body)
			if status != tc.status || !strings.Contains(string(response), tc.contains) {
				t.Fatalf("rejection changed: %d %s", status, response)
			}
			var row model.PluginInstallation
			if err := f.h.db.First(&row, "id = ?", f.installation.ID).Error; err != nil || row.Enabled {
				t.Fatalf("failure enabled plugin: %v %+v", err, row)
			}
			operations, err := f.manager.Operations(f.installation.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, operation := range operations {
				if operation.Action == "enable" && operation.State == "succeeded" {
					t.Fatal("failed request recorded as success")
				}
			}
		})
	}
}
