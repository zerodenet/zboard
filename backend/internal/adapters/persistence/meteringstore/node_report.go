package meteringstore

import (
	"context"
	"errors"
	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type NodeReports struct {
	DB         *gorm.DB
	Expire     func(*gorm.DB, uint, time.Time) error
	QuotaEvent func(*gorm.DB, model.Subscription, string, int64, int64, int64, string, string) error
}

func (s NodeReports) Record(ctx context.Context, in metering.AuthenticatedNodeReport) (metering.NodeReportResult, error) {
	var sub model.Subscription
	var record model.TrafficRecord
	duplicate := false
	quotaExhausted := false
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var lockedNode model.Node
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedNode, in.NodeID).Error; err != nil {
			return err
		}
		if !lockedNode.IsEnabled || lockedNode.TrafficSecret == "" || lockedNode.TrafficSecretRevokedAt != nil || lockedNode.TrafficSecret != in.ExpectedCredential {
			return metering.ErrNodeReportCredentialChanged
		}
		nodeUpdates := map[string]interface{}{
			"is_online": true, "status": 1,
			"last_seen_at": in.Timestamp, "last_sync_at": in.Timestamp,
		}
		if in.Version != "" {
			nodeUpdates["version"] = in.Version
		}
		if err := tx.Model(&lockedNode).Updates(nodeUpdates).Error; err != nil {
			return err
		}
		var endpoint model.ProtocolEndpoint
		if err := tx.Where("id = ? AND node_id = ? AND is_active = ?", in.ProtocolEndpointID, lockedNode.ID, true).First(&endpoint).Error; err != nil {
			return metering.ErrProtocolEndpointUnavailable
		}

		err := tx.Where("node_id = ? AND report_id = ?", lockedNode.ID, in.ReportID).First(&record).Error
		if err == nil {
			duplicate = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		var nonceRecord model.TrafficRecord
		err = tx.Where("node_id = ? AND nonce = ?", lockedNode.ID, in.Nonce).First(&nonceRecord).Error
		if err == nil {
			return metering.ErrNodeReportNonceReplayed
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		now := time.Now().UTC()
		if err := s.Expire(tx, in.UserID, now); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Joins("JOIN (SELECT node_group_id, protocol_endpoint_id FROM node_group_endpoints) AS node_group_endpoints ON node_group_endpoints.node_group_id = subscriptions.node_group_id").
			Where("subscriptions.user_id = ? AND subscriptions.status = ? AND subscriptions.end_at > ? AND node_group_endpoints.protocol_endpoint_id = ?", in.UserID, "active", now, endpoint.ID).
			Order("end_at desc").First(&sub).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return metering.ErrSubscriptionNotFound
			}
			return err
		}

		remaining := sub.FlowTotal - sub.FlowUsed
		if remaining <= 0 {
			if err := tx.Model(&sub).Update("status", "expired").Error; err != nil {
				return err
			}
			quotaExhausted = true
			return nil
		}
		rawBytes := metering.TrafficBytesForMode(in.UploadBytes, in.DownloadBytes, sub.TrafficCalcMode)
		if rawBytes <= 0 {
			return metering.ErrNoBillableTraffic
		}
		used, err := metering.BilledTrafficBytes(rawBytes, endpoint.MultiplierMilli)
		if err != nil {
			return err
		}
		if used > remaining {
			used = remaining
		}
		sub.FlowUsed += used
		if sub.FlowUsed >= sub.FlowTotal {
			sub.Status = "expired"
		}
		if err := tx.Save(&sub).Error; err != nil {
			return err
		}
		if err := s.QuotaEvent(tx, sub, "usage", -used, remaining, remaining-used, "traffic_report", in.ReportID); err != nil {
			return err
		}
		record = model.TrafficRecord{
			UserID:                  in.UserID,
			SubscriptionID:          sub.ID,
			NodeID:                  lockedNode.ID,
			ProtocolEndpointID:      endpoint.ID,
			ReportID:                in.ReportID,
			Nonce:                   in.Nonce,
			RawBytes:                rawBytes,
			UploadBytes:             in.UploadBytes,
			DownloadBytes:           in.DownloadBytes,
			TrafficCalcMode:         sub.TrafficCalcMode,
			ProtocolMultiplierMilli: endpoint.MultiplierMilli,
			UsedBytes:               used,
			At:                      in.Timestamp,
			Meta:                    in.Meta,
		}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		return AddProtocolEndpointUsage(tx, []model.TrafficRecord{record})
	})
	if err != nil {
		return metering.NodeReportResult{}, err
	}
	cycleTotal, cycleUsed := entitlements.CycleQuota(entitlements.Subscription(sub))
	return metering.NodeReportResult{Record: metering.TrafficRecord(record), Duplicate: duplicate, QuotaExhausted: quotaExhausted, FlowUsed: cycleUsed, FlowTotal: cycleTotal, SubscriptionEnd: sub.EndAt}, nil
}
