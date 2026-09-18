package meteringstore

import (
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func FlowCursor(row model.FlowUsage) metering.FlowCursor {
	return metering.FlowCursor{CredentialID: row.ProtocolCredentialID, Revision: row.Revision, Status: row.Status, Counters: metering.FlowCounters{Raw: row.RawBytes, Upload: row.UploadBytes, Download: row.DownloadBytes}}
}
func PickFlowUsage(candidates []model.FlowUsage, key, legacy string) (model.FlowUsage, bool, bool) {
	for _, row := range candidates {
		if row.FlowID == key {
			return row, true, false
		}
	}
	if key != legacy {
		for _, row := range candidates {
			if row.FlowID == legacy {
				return row, true, true
			}
		}
	}
	return model.FlowUsage{}, false, false
}
func LoadFlowUsage(tx *gorm.DB, node uint, instance, flow string, credential uint, revision uint64, current metering.FlowCounters) (model.FlowUsage, bool, bool, error) {
	key := metering.FlowUsageKey(instance, flow)
	keys := []string{key}
	if key != flow {
		keys = append(keys, flow)
	}
	var candidates []model.FlowUsage
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ? AND flow_id IN ?", node, keys).Find(&candidates).Error; err != nil {
		return model.FlowUsage{}, false, false, err
	}
	row, found, legacy := PickFlowUsage(candidates, key, flow)
	if !found {
		return model.FlowUsage{}, false, false, nil
	}
	if legacy && !metering.ContinuesLegacyFlow(FlowCursor(row), credential, revision, current) {
		return model.FlowUsage{}, false, false, nil
	}
	return row, true, legacy, nil
}
