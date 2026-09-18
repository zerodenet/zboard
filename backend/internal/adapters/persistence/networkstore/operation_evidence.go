package networkstore

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/jobstore"
	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"gorm.io/gorm"
)

// The outer query must select the operation table belonging to kind. Both
// explicit removal and age-based retention preserve the same unresolved facts.
func unconfirmedOperationEvidence(tx *gorm.DB, kind network.OperationKind) *gorm.DB {
	table := "provider_operations"
	if kind == network.CertificateOperation {
		table = "certificate_operations"
	}
	key := "(? || " + table + ".id)"
	if tx.Dialector.Name() == "mysql" {
		key = "CONCAT(?, " + table + ".id)"
	}
	return tx.Model(&jobstore.Record{}).Select("1").Where("owner = ? AND handler = ? AND `key` = "+key, "system", string(kind), string(kind)+":").Where("(state NOT IN ? OR (state IN ? AND state <> "+table+".status))", []jobs.State{jobs.Succeeded, jobs.Failed, jobs.Canceled}, []jobs.State{jobs.Succeeded, jobs.Failed})
}
