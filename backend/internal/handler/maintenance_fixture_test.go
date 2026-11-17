package handler

import (
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
)

// prepareTestMaintenanceSource is an explicit normal-source prerequisite for
// the individually selected positive SQLite/store fixtures. Use the existing
// production definition/initializer without populating unrelated custom keys or
// inventing a maintenance_task_id. Never invoke it from reads, assertions,
// shared factories or after a test deliberately damages its initialized source.
func prepareTestMaintenanceSource(t testing.TB, h *handlers) {
	t.Helper()
	for _, definition := range platform.DefaultSystemConfigs() {
		if definition.Key != "maintenance_enabled" {
			continue
		}
		if err := h.services.SystemConfigDefaults.Repository.ReconcileSystemConfigDefaults(t.Context(), []platform.SystemConfigDefault{definition}); err != nil {
			t.Fatal(err)
		}
		h.services.InvalidateMaintenance()
		return
	}
	t.Fatal("production maintenance_enabled definition is missing")
}
