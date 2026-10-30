package handler

import (
	"bytes"
	"encoding/json"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/networkstore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"testing"
)

func TestArchivedBufferedFlowRetainsSourceWithoutChargingOrBlockingHealthyNode(t *testing.T) {
	h, credential := accountingBenchmarkFixture(t)
	var original model.Node
	if err := h.db.First(&original, credential.NodeID).Error; err != nil {
		t.Fatal(err)
	}
	late := accountingBenchmarkEvents(credential, 0, 1)
	late[0].AssetTag = original.AssetTag
	if err := h.projectZeroNodeEvents(t.Context(), late); err != nil {
		t.Fatal(err)
	}
	// Create a separate live identity/credential on the same subscription.
	healthy := model.Node{Name: "healthy-after-archive", Config: "{}"}
	if err := h.db.Create(&healthy).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{NodeID: healthy.ID, Name: "healthy", RuntimeKey: "healthy-archive", Protocol: "vless", OptionalConfig: "{}", Tags: "[]"}
	if err := h.db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	live := credential
	live.ID = 0
	live.NodeID = healthy.ID
	live.ProtocolEndpointID = endpoint.ID
	live.CredentialID = "healthy-archive"
	live.PrincipalKey = "healthy-archive"
	live.PrincipalLookupVersion = 0
	live.PrincipalDigest = ""
	live.SecretPrincipalDigest = "" // rebuilt by the normal principal-index preparation
	if err := h.db.Create(&live).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&model.User{}).Where("id = ?", credential.UserID).Update("is_admin", true).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := (networkstore.NodeRemoval{DB: h.db}).RemoveNode(t.Context(), credential.UserID, original.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.db.Where("node_id = ?", original.ID).Delete(&model.FlowUsage{}).Error; err != nil {
		t.Fatal(err)
	}
	late = accountingBenchmarkEvents(credential, 1, 1)
	late[0].AssetTag = original.AssetTag
	late[0].ID = "closed-report-must-be-held"
	late[0].Payload = json.RawMessage(`{"flow_id":"flow-0","traffic":{"bytes_up":100,"bytes_down":200}}`)
	good := accountingBenchmarkEvents(live, 2, 1)
	good[0].AssetTag = healthy.AssetTag
	if err := h.projectZeroNodeEvents(t.Context(), append(late, good...)); err != nil {
		t.Fatal("closed source blocked healthy settlement", err)
	}
	var total int64
	if err := h.db.Model(&model.TrafficRecord{}).Where("node_id = ?", original.ID).Count(&total).Error; err != nil || total != 1 {
		t.Fatal("archive booked a late unknown report", total, err)
	}
	if err := h.db.Model(&model.TrafficRecord{}).Where("node_id = ?", healthy.ID).Count(&total).Error; err != nil || total != 1 {
		t.Fatal("healthy financial settlement absent", total, err)
	}
	var parts []model.NodeObservationPart
	if err := h.db.Joins("JOIN node_observation_intents h ON h.id = node_observation_parts.intent_id").Where("h.node_id = ?", original.ID).Find(&parts).Error; err != nil {
		t.Fatal(err)
	}
	retained := false
	for _, part := range parts {
		retained = retained || bytes.Contains(part.Data, []byte(late[0].ID))
	}
	if !retained {
		t.Fatal("original late source was discarded")
	}
	if _, err := h.services.NodeObservations.Drain(t.Context(), 32, 8*1024*1024); err != nil {
		t.Fatal(err)
	}
	if err := h.db.Model(&model.NodeObservationIntent{}).Where("node_id = ? AND status = ? AND reason = ?", original.ID, "held", "node_archived").Count(&total).Error; err != nil || total == 0 {
		t.Fatal("late source lacks archive hold reason", total, err)
	}

	if err := h.db.Model(&model.FlowUsage{}).Where("node_id = ?", original.ID).Count(&total).Error; err != nil || total != 0 {
		t.Fatal("late report recreated archived cursor", total, err)
	}
	// Replaying the same window never doubles the healthy charge.
	if err := h.projectZeroNodeEvents(t.Context(), append(late, good...)); err != nil {
		t.Fatal(err)
	}
	var sub model.Subscription
	if err := h.db.First(&sub, credential.SubscriptionID).Error; err != nil {
		t.Fatal(err)
	}
	if sub.FlowUsed != 120 {
		t.Fatal("archive/replay changed quota", sub.FlowUsed)
	}
}
