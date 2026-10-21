package networkstore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/observationstore"
	"gorm.io/gorm/clause"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

type NodeActivity struct{ DB *gorm.DB }

func (s NodeActivity) RecordNodeActivity(ctx context.Context, nodeID uint, credential string, update network.NodeActivityUpdate) error {
	values := map[string]any{
		"last_seen_at": update.At, "is_online": update.Online,
		"status": map[bool]int{false: 0, true: 1}[update.Online],
	}
	if update.ConnectorSeen {
		values["connector_last_seen_at"] = update.At
	} else {
		values["connector_last_seen_at"] = nil
	}
	if update.Version != nil && *update.Version != "" {
		values["version"] = *update.Version
	}
	if update.UptimeSeconds != nil {
		values["uptime_seconds"] = *update.UptimeSeconds
	}
	if update.ActiveFlows != nil {
		values["active_flows"] = *update.ActiveFlows
	}
	if update.BytesUp != nil {
		values["bytes_up"] = *update.BytesUp
	}
	if update.BytesDown != nil {
		values["bytes_down"] = *update.BytesDown
	}

	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var node model.Node
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND is_enabled = ? AND node_credential = ? AND node_credential_revoked_at IS NULL", nodeID, true, credential).First(&node).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return network.ErrNodeActivityCredential
		}
		if err != nil {
			return err
		}
		if update.ActiveFlows != nil || update.BytesUp != nil || update.BytesDown != nil {
			source := "unknown"
			switch update.CounterWriter {
			case network.CounterWriterHeartbeat, network.CounterWriterLegacyConnector:
				source = string(update.CounterWriter)
			}
			if err := observationstore.CounterOverride(tx, node.ID, node.AssetTag, source, update.At); err != nil {
				return err
			}
		}
		return tx.Model(&model.Node{}).Where("id = ?", nodeID).Updates(values).Error
	})
}
