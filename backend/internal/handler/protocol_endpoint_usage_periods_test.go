package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestProtocolEndpointUsageResetHandlerKeepsDailyProjection(t *testing.T) {
	f := newTrafficReadFixture(t)
	now := time.Now().UTC()
	node := model.Node{Name: "node", Address: "node.example.test", Config: "{}"}
	if err := f.h.db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := model.ProtocolEndpoint{NodeID: node.ID, Name: "endpoint", Protocol: "vless", Address: node.Address, Port: 443, PublicPort: 443, ClientConfig: "{}", OptionalConfig: "{}", Tags: "[]"}
	if err := f.h.db.Create(&endpoint).Error; err != nil {
		t.Fatal(err)
	}
	usage := model.ProtocolEndpointUsageDaily{ProtocolEndpointID: endpoint.ID, UsageDate: now.Format("2006-01-02"), UsedBytes: 512, LastRecordAt: now, UpdatedAt: now}
	if err := f.h.db.Create(&usage).Error; err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/v1/admin/protocol-endpoints/%d/usage-resets", endpoint.ID)
	reset := httptest.NewRecorder()
	f.h.ProtocolEndpointUsageResetsHandler(reset, announcementRequest(http.MethodPost, path, f.admin, `{"reason":"更换端口"}`))
	if reset.Code != http.StatusOK {
		t.Fatalf("reset status=%d body=%s", reset.Code, reset.Body.String())
	}
	var response struct {
		Data struct {
			PeriodUsedBytes int64  `json:"period_used_bytes"`
			Reason          string `json:"reason"`
		} `json:"data"`
	}
	if err := json.Unmarshal(reset.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.PeriodUsedBytes != 512 || response.Data.Reason != "更换端口" {
		t.Fatalf("reset=%+v", response.Data)
	}
	history := httptest.NewRecorder()
	f.h.ProtocolEndpointUsageResetsHandler(history, announcementRequest(http.MethodGet, path, f.admin, ""))
	if history.Code != http.StatusOK || !json.Valid(history.Body.Bytes()) {
		t.Fatalf("history status=%d body=%s", history.Code, history.Body.String())
	}
	invalid := httptest.NewRecorder()
	f.h.ProtocolEndpointUsageResetsHandler(invalid, announcementRequest(http.MethodPost, path, f.admin, `{"reason":" "}`))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status=%d body=%s", invalid.Code, invalid.Body.String())
	}
	forbidden := httptest.NewRecorder()
	f.h.ProtocolEndpointUsageResetsHandler(forbidden, announcementRequest(http.MethodPost, path, f.token, `{"reason":"unauthorized"}`))
	if forbidden.Code == http.StatusOK {
		t.Fatalf("non-admin reset allowed: %s", forbidden.Body.String())
	}
	var stored model.ProtocolEndpointUsageDaily
	if err := f.h.db.First(&stored, "protocol_endpoint_id = ?", endpoint.ID).Error; err != nil || stored.UsedBytes != 512 {
		t.Fatalf("daily=%+v err=%v", stored, err)
	}
}
