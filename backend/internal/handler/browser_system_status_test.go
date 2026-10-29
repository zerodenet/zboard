package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestBrowserSystemStatusRequiresInitializedMaintenanceSource(t *testing.T) {
	h, _ := newAnnouncementTestHandlers(t)
	read := func() int {
		response := httptest.NewRecorder()
		h.SystemStatusHandler(response, httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil))
		return response.Code
	}
	if status := read(); status != http.StatusInternalServerError {
		t.Fatalf("uninitialized source status=%d", status)
	}
	// Use the existing production definition/initializer, only in this new
	// positive fixture. Never synthesize missing configuration from a read.
	prepareTestMaintenanceSource(t, h)
	if status := read(); status != http.StatusOK {
		t.Fatalf("initialized source status=%d", status)
	}
	if err := h.db.Where("config_key = ?", "maintenance_enabled").Delete(&model.SystemConfig{}).Error; err != nil {
		t.Fatal(err)
	}
	if status := read(); status != http.StatusInternalServerError {
		t.Fatalf("lost initialized source status=%d", status)
	}
	var remaining int64
	if err := h.db.Model(&model.SystemConfig{}).Where("config_key = ?", "maintenance_enabled").Count(&remaining).Error; err != nil || remaining != 0 {
		t.Fatal("status read reconstructed lost maintenance evidence", err)
	}
}
