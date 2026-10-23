package handler

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observationstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestObservationIntentOperationsHTTP(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	observationIntentOperationsHTTP(t, h)
}
func TestMySQLObservationIntentOperationsHTTP(t *testing.T) {
	if os.Getenv("ZBOARD_TEST_MYSQL_DSN") == "" {
		t.Skip("actual MySQL DSN required")
	}
	h, _ := newMySQLPublishHandlers(t)
	observationIntentOperationsHTTP(t, h)
}
func observationIntentOperationsHTTP(t *testing.T, h *handlers) {
	admin := model.User{Email: "ops-admin@example.test", Password: "unused", Status: "active", IsAdmin: true}
	ordinary := model.User{Email: "ops-user@example.test", Password: "unused", Status: "active"}
	for _, u := range []*model.User{&admin, &ordinary} {
		if err := h.db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	token := func(user model.User) string {
		v, _, err := h.issueToken(authClaims{UserID: user.ID, Email: user.Email, IsAdmin: user.IsAdmin})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	adminToken, userToken := token(admin), token(ordinary)
	const nodeCredential = "not-a-browser-identity"
	encrypted, err := h.credentialCipher.Encrypt(nodeCredential)
	if err != nil {
		t.Fatal(err)
	}
	node := model.Node{Name: "deleted-observation-node", Config: "{}", NodeCredential: encrypted, IsEnabled: true}
	if err := h.db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 21, 1, 2, 3, 456000000, time.UTC)
	diagnostic := map[string]string{}
	for i := uint64(1); i <= 3; i++ {
		p := network.EventPosition{AssetTag: node.AssetTag, ID: string(rune('a' + i)), CoreInstanceID: "private-core", SourceID: "private-source", Sequence: i, OccurredAt: at, ReceivedAt: at}
		source := network.ObservationSource{Observation: network.NodeObservation{NodeID: node.ID, Latest: p}, Kind: "flow.updated", PrincipalKey: "private-principal", FlowID: "private-flow", Payload: []byte("private-payload")}
		var id string
		if err := h.db.Transaction(func(tx *gorm.DB) error { var err error; id, err = observationstore.Enqueue(tx, source); return err }); err != nil {
			t.Fatal(err)
		}
		if err := h.db.Model(&model.NodeObservationIntent{}).Where("id = ?", id).Updates(map[string]any{"status": "held", "available_at": at, "reason": "unknown_source", "token": "private-token"}).Error; err != nil {
			t.Fatal(err)
		}
		text := ""
		if i == 1 {
			text = "9007199254740993"
		} else if i == 2 && h.db.Dialector.Name() == "mysql" {
			text = "18446744073709551615"
		}
		if text != "" {
			if err := h.db.Model(&model.NodeObservationIntent{}).Where("id = ?", id).Updates(map[string]any{"attempts": text, "material_bytes": text, "part_count": text}).Error; err != nil {
				t.Fatal(err)
			}
			var stored model.NodeObservationIntent
			if err := h.db.First(&stored, "id = ?", id).Error; err != nil {
				t.Fatal(err)
			}
			if strconv.FormatUint(stored.Attempts, 10) != text || strconv.FormatUint(stored.MaterialBytes, 10) != text || strconv.FormatUint(stored.PartCount, 10) != text {
				t.Fatal("physical unsigned diagnostic scalar changed")
			}
			diagnostic[id] = text
		}
	}
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := observationstore.Tombstone(tx, node.ID, node.AssetTag); err != nil {
			return err
		}
		return tx.Delete(&node).Error
	}); err != nil {
		t.Fatal(err)
	}
	request := func(query, credential string, expected int) network.ObservationIntentPage {
		t.Helper()
		w := httptest.NewRecorder()
		h.NodeObservationIntentsHandler(w, announcementRequest(http.MethodGet, "/api/v1/admin/node-observation-intents"+query, credential, ""))
		if w.Code != expected {
			t.Fatalf("query=%s HTTP=%d expected=%d body=%s", query, w.Code, expected, w.Body.String())
		}
		for _, value := range []string{nodeCredential, node.AssetTag, "private-token", "private-core", "private-source", "private-principal", "private-flow", "private-payload", "deleted-observation-node"} {
			if strings.Contains(w.Body.String(), value) {
				t.Fatal("header API leaked private/historical source data")
			}
		}
		var envelope struct {
			Data network.ObservationIntentPage `json:"data"`
		}
		if expected == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
		}
		return envelope.Data
	}
	first := request("?limit=2", adminToken, 200)
	if first.Status != "held" || len(first.Items) != 2 || !first.HasMore || first.NextCursor == "" {
		t.Fatal(first)
	}
	second := request("?limit=2&cursor="+first.NextCursor, adminToken, 200)
	if len(second.Items) != 1 || second.HasMore || second.Items[0].ID == first.Items[1].ID {
		t.Fatal(second)
	}
	checked := 0
	for _, item := range append(first.Items, second.Items...) {
		if text, ok := diagnostic[item.ID]; ok {
			checked++
			if item.Attempts != text || item.MaterialBytes != text || item.PartCount != text {
				t.Fatal("row-to-HTTP diagnostics lost precision", item)
			}
		}
		if item.AssetRelation != "asset_deleted" || item.MaterialVerified || item.NodeID == "" {
			t.Fatal(item)
		}
	}
	if checked != len(diagnostic) {
		t.Fatal("did not read all physical scalar boundary rows", checked)
	}
	for _, query := range []string{"?status=all", "?status=", "?status=HELD", "?limit=0", "?limit=101", "?limit=no", "?cursor=", "?cursor=not-a-cursor", "?status=held&status=pending", "?node_id=1", "?reason=unknown_source", "?offset=0", "?status=pending&cursor=" + first.NextCursor} {
		request(query, adminToken, 400)
	}
	request("", userToken, 403)
	request("", nodeCredential, 401)
	request("", "", 401)
	if err := h.db.Model(&admin).Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	request("", adminToken, 403)
	if err := h.db.Model(&admin).Updates(map[string]any{"is_admin": true, "status": "suspended"}).Error; err != nil {
		t.Fatal(err)
	}
	request("", adminToken, 401)
	if err := h.db.Model(&admin).Update("status", "active").Error; err != nil {
		t.Fatal(err)
	}
	// Repository authorization remains real even if an adapter passes a stale
	// role bit. It is not solely the HTTP/session gate.
	if _, err := h.services.ObservationIntents.List(context.Background(), ordinary.ID, network.ObservationIntentQuery{}); !errors.Is(err, network.ErrNodeAdministrationPermission) {
		t.Fatal(err)
	}
	var before []model.NodeObservationIntent
	if err := h.db.Order("id").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	injected := errors.New("observation read failure")
	const hook = "test:observation_http_read_failure"
	if err := h.db.Callback().Row().Before("gorm:row").Register(hook, func(tx *gorm.DB) {
		if tx.Statement.TableExpr != nil && strings.Contains(tx.Statement.TableExpr.SQL, "node_observation_intents") {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	request("", adminToken, 500)
	_ = h.db.Callback().Row().Remove(hook)
	var after []model.NodeObservationIntent
	if err := h.db.Order("id").Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed read changed queue")
	}
}
