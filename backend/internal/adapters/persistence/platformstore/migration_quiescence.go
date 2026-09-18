package platformstore

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/platform"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// MigrationSourceQuiescent is the transitional cross-domain read projection;
// domain inboxes remain authoritative until their capability migration completes.
func MigrationSourceQuiescent(db *gorm.DB, runID string) error {
	checks := []struct {
		name  string
		model interface{}
		where string
		args  []interface{}
	}{
		{name: "shared job executions", model: &jobstore.Record{}, where: "state IN ? AND id <> ?", args: []interface{}{[]string{"running", "unknown"}, runID}},
		{name: "background tasks", model: &model.Task{}, where: "type <> ? AND status IN ?", args: []interface{}{"database_migration", []int16{0, 1}}},
		{name: "node operations", model: &model.NodeOperation{}, where: "status = ?", args: []interface{}{"running"}},
		{name: "provider operations", model: &model.ProviderOperation{}, where: "status = ?", args: []interface{}{"running"}},
		{name: "certificate operations", model: &model.CertificateOperation{}, where: "status = ?", args: []interface{}{"running"}},
		{name: "protocol deployments", model: &model.ProtocolDeployment{}, where: "status = ?", args: []interface{}{"running"}},
	}
	for _, check := range checks {
		var count int64
		if err := db.Model(check.model).Where(check.where, check.args...).Count(&count).Error; err != nil {
			return fmt.Errorf("inspect %s: %w", check.name, err)
		}
		if count > 0 {
			return fmt.Errorf("%w: %d %s must finish before migration", platform.ErrMigrationBusy, count, check.name)
		}
	}
	return nil
}
