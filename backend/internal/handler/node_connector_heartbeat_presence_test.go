package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

func TestNodeConnectorHeartbeatPreservesMissingObservations(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	assertHeartbeatPresence(t, h)
}

func TestMySQLNodeConnectorHeartbeatPreservesMissingObservations(t *testing.T) {
	// One real isolated schema/Prepare for the whole matrix, not per case.
	h, _ := newMySQLPublishHandlers(t)
	assertHeartbeatPresence(t, h)
}

func heartbeatPresenceSnapshot(t *testing.T, db *gorm.DB, node uint) map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := db.Model(&model.Node{}).Where("id = ?", node).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatal("expected one persisted node row", len(rows))
	}
	return rows[0]
}

func heartbeatPresenceOnlyChanges(t *testing.T, before, after map[string]any, allowed map[string]bool) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatal("persisted node column set changed")
	}
	for column, value := range before {
		if !allowed[column] && !reflect.DeepEqual(value, after[column]) {
			t.Fatal("unexpected persisted node column change", column)
		}
	}
}

func assertHeartbeatPresence(t *testing.T, h *handlers) {
	t.Helper()
	const secret = "heartbeat-presence-secret"
	encrypted, err := h.credentialCipher.Encrypt(secret)
	if err != nil {
		t.Fatal(err)
	}
	nodeSequence := 0
	newNode := func(t *testing.T) model.Node {
		t.Helper()
		nodeSequence++
		old := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
		node := model.Node{Name: fmt.Sprintf("heartbeat-presence-%d", nodeSequence), Config: "{}", IsEnabled: true,
			NodeCredential: encrypted, NodeCredentialPrefix: secret[:12], Version: "known-build",
			UptimeSeconds: 11, ActiveFlows: 22, BytesUp: 33, BytesDown: 44,
			LastSeenAt: &old, ConnectorLastSeenAt: &old}
		if err := h.db.Create(&node).Error; err != nil {
			t.Fatal(err)
		}
		return node
	}
	readNode := func(t *testing.T, id uint) model.Node {
		t.Helper()
		var node model.Node
		if err := h.db.First(&node, id).Error; err != nil {
			t.Fatal(err)
		}
		return node
	}
	request := func(id uint, token, raw string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/nodes/%d/heartbeat", id), strings.NewReader(raw))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.NodeConnectorHeartbeatHandler(w, r)
		return w
	}
	encode := func(t *testing.T, body map[string]any) string {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	fields := []string{"uptime_seconds", "active_flows", "bytes_up", "bytes_down"}
	accepted := func(t *testing.T, id uint, body map[string]any) {
		t.Helper()
		before := heartbeatPresenceSnapshot(t, h.db, id)
		prior := readNode(t, id)
		w := request(id, secret, encode(t, body))
		if w.Code != http.StatusOK {
			t.Fatalf("heartbeat status %d: %s", w.Code, w.Body.String())
		}
		var response map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || len(response) != 1 || response["ok"] != true {
			t.Fatal("legacy plain success response changed", err)
		}
		allowed := map[string]bool{"last_seen_at": true, "connector_last_seen_at": true, "is_online": true, "status": true, "updated_at": true}
		want := []uint64{prior.UptimeSeconds, prior.ActiveFlows, prior.BytesUp, prior.BytesDown}
		for index, field := range fields {
			if value, provided := body[field]; provided && value != nil {
				want[index] = value.(uint64)
				allowed[field] = true
			}
		}
		wantVersion := prior.Version
		if build, ok := body["build_id"].(string); ok && strings.TrimSpace(build) != "" {
			wantVersion = strings.TrimSpace(build)
			allowed["version"] = true
		}
		after := readNode(t, id)
		if got := []uint64{after.UptimeSeconds, after.ActiveFlows, after.BytesUp, after.BytesDown}; !reflect.DeepEqual(got, want) {
			t.Fatal("heartbeat counter presence mismatch", got, want)
		}
		if after.Version != wantVersion || !after.IsOnline || after.Status != 1 || after.LastSeenAt == nil || after.ConnectorLastSeenAt == nil || after.LastSeenAt.Before(*prior.LastSeenAt) || !after.LastSeenAt.Equal(*after.ConnectorLastSeenAt) {
			t.Fatal("valid partial heartbeat lost existing version/liveness behavior")
		}
		heartbeatPresenceOnlyChanges(t, before, heartbeatPresenceSnapshot(t, h.db, id), allowed)
		var cursors int64
		if err := h.db.Table("zero_event_node_cursors").Where("node_id = ?", id).Count(&cursors).Error; err != nil {
			t.Fatal(err)
		}
		if cursors != 0 {
			t.Fatal("heartbeat fabricated an event cursor")
		}
	}
	t.Run("known partial null mixed and explicit zero sequence", func(t *testing.T) {
		node := newNode(t)
		id := fmt.Sprint(node.ID)
		accepted(t, node.ID, map[string]any{"node_id": id})
		accepted(t, node.ID, map[string]any{"node_id": id, "build_id": nil, "uptime_seconds": nil, "active_flows": nil, "bytes_up": nil, "bytes_down": nil})
		accepted(t, node.ID, map[string]any{"node_id": id, "build_id": "", "uptime_seconds": uint64(0), "active_flows": nil, "bytes_down": uint64(99)})
		accepted(t, node.ID, map[string]any{"node_id": id, "build_id": " fresh-build ", "uptime_seconds": uint64(0), "active_flows": uint64(0), "bytes_up": uint64(0), "bytes_down": uint64(0), "future_field": true})
	})
	for _, field := range fields {
		for _, kind := range []string{"missing", "null", "zero"} {
			t.Run(field+"/"+kind, func(t *testing.T) {
				node := newNode(t)
				body := map[string]any{"node_id": fmt.Sprint(node.ID), "build_id": "known-build", "uptime_seconds": uint64(101), "active_flows": uint64(202), "bytes_up": uint64(303), "bytes_down": uint64(404)}
				switch kind {
				case "missing":
					delete(body, field)
				case "null":
					body[field] = nil
				case "zero":
					body[field] = uint64(0)
				}
				accepted(t, node.ID, body)
			})
		}
	}
	t.Run("bad token and invalid bodies do not write", func(t *testing.T) {
		node := newNode(t)
		valid := fmt.Sprintf(`{"node_id":"%d","build_id":"known-build","uptime_seconds":0,"active_flows":0,"bytes_up":0,"bytes_down":0}`, node.ID)
		cases := []struct {
			name, token, body string
			status            int
		}{
			{"bad token", "wrong-secret", valid, http.StatusUnauthorized},
			{"missing node", secret, `{}`, http.StatusBadRequest},
			{"wrong node", secret, `{"node_id":"999999"}`, http.StatusBadRequest},
			{"negative", secret, strings.Replace(valid, `"uptime_seconds":0`, `"uptime_seconds":-1`, 1), http.StatusBadRequest},
			{"fraction", secret, strings.Replace(valid, `"active_flows":0`, `"active_flows":1.5`, 1), http.StatusBadRequest},
			{"overflow", secret, strings.Replace(valid, `"bytes_up":0`, `"bytes_up":18446744073709551616`, 1), http.StatusBadRequest},
			{"wrong type", secret, strings.Replace(valid, `"bytes_down":0`, `"bytes_down":"0"`, 1), http.StatusBadRequest},
			{"malformed", secret, `{`, http.StatusBadRequest},
			{"trailing object", secret, valid + ` {}`, http.StatusBadRequest},
			{"oversized", secret, strings.Repeat(" ", nodeReportMaxBodyBytes+1), http.StatusBadRequest},
		}
		for _, item := range cases {
			t.Run(item.name, func(t *testing.T) {
				before := heartbeatPresenceSnapshot(t, h.db, node.ID)
				if w := request(node.ID, item.token, item.body); w.Code != item.status {
					t.Fatal("unexpected rejection status", w.Code, item.status)
				}
				heartbeatPresenceOnlyChanges(t, before, heartbeatPresenceSnapshot(t, h.db, node.ID), nil)
			})
		}
	})
	t.Run("rotated credential fences already authenticated request", func(t *testing.T) {
		node := newNode(t)
		rotated, err := h.credentialCipher.Encrypt("rotated-heartbeat-secret")
		if err != nil {
			t.Fatal(err)
		}
		callback := "test:heartbeat_presence_rotate"
		var rotatedRow map[string]any
		if err := h.db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
			if tx.Statement.Table != "nodes" || rotatedRow != nil {
				return
			}
			// Actual rotation between auth read and conditional UPDATE, on the
			// same transaction connection; no recursive update callbacks.
			if err := tx.Session(&gorm.Session{NewDB: true}).Exec("UPDATE nodes SET node_credential = ? WHERE id = ?", rotated, node.ID).Error; err != nil {
				tx.AddError(err)
				return
			}
			rotatedRow = heartbeatPresenceSnapshot(t, tx.Session(&gorm.Session{NewDB: true}), node.ID)
		}); err != nil {
			t.Fatal(err)
		}
		defer h.db.Callback().Update().Remove(callback)
		w := request(node.ID, secret, fmt.Sprintf(`{"node_id":"%d","uptime_seconds":0,"active_flows":0,"bytes_up":0,"bytes_down":0}`, node.ID))
		if w.Code != http.StatusUnauthorized || rotatedRow == nil {
			t.Fatal("rotation did not fence in-flight old credential", w.Code)
		}
		heartbeatPresenceOnlyChanges(t, rotatedRow, heartbeatPresenceSnapshot(t, h.db, node.ID), nil)
	})
	t.Run("storage failure remains an error without partial observation", func(t *testing.T) {
		node := newNode(t)
		before := heartbeatPresenceSnapshot(t, h.db, node.ID)
		callback := "test:heartbeat_presence_failure"
		if err := h.db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
			if tx.Statement.Table == "nodes" {
				tx.AddError(errors.New("injected heartbeat write failure"))
			}
		}); err != nil {
			t.Fatal(err)
		}
		defer h.db.Callback().Update().Remove(callback)
		w := request(node.ID, secret, fmt.Sprintf(`{"node_id":"%d","uptime_seconds":0,"active_flows":0,"bytes_up":0,"bytes_down":0}`, node.ID))
		if w.Code != http.StatusInternalServerError {
			t.Fatal("write error became successful heartbeat", w.Code)
		}
		heartbeatPresenceOnlyChanges(t, before, heartbeatPresenceSnapshot(t, h.db, node.ID), nil)
	})
}
