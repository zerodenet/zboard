package networkstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ProtocolEndpointUsagePeriods struct{ DB *gorm.DB }

func (s ProtocolEndpointUsagePeriods) ResetProtocolEndpointUsage(ctx context.Context, actor, endpointID uint, reason string, now time.Time) (out network.ProtocolEndpointUsageResetRecord, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		admin, err := providerAdmin(tx, actor)
		if errors.Is(err, network.ErrProviderPermission) {
			return network.ErrProtocolEndpointUsagePermission
		}
		if err != nil {
			return err
		}
		var endpoint model.ProtocolEndpoint
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&endpoint, endpointID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return network.ErrProtocolEndpointUsageNotFound
			}
			return err
		}
		day := now.UTC().Format("2006-01-02")
		var totals struct {
			Total int64
			Today int64
		}
		if err := tx.Model(&model.ProtocolEndpointUsageDaily{}).
			Select("COALESCE(SUM(used_bytes),0) AS total, COALESCE(SUM(CASE WHEN usage_date = ? THEN used_bytes ELSE 0 END),0) AS today", day).
			Where("protocol_endpoint_id = ?", endpointID).Scan(&totals).Error; err != nil {
			return err
		}
		var previous model.ProtocolEndpointUsageReset
		err = tx.Where("protocol_endpoint_id = ?", endpointID).Order("id desc").First(&previous).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		periodBytes := totals.Total - previous.BaselineTotalBytes
		if periodBytes < 0 {
			periodBytes = 0
		}
		reset := model.ProtocolEndpointUsageReset{
			ProtocolEndpointID: endpointID, ResetAt: now.UTC(), UsageDate: day,
			BaselineTotalBytes: totals.Total, BaselineTodayBytes: totals.Today,
			PeriodUsedBytes: periodBytes, Reason: reason, ActorUserID: admin.ID,
		}
		if err := tx.Create(&reset).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.AuditLog{
			UserID: &admin.ID, Actor: admin.Email, Action: "protocol_endpoint.usage_reset",
			Target: fmt.Sprintf("protocol_endpoint:%d", endpointID),
			Detail: fmt.Sprintf("reset_id=%d closed_period_bytes=%d reason=%q", reset.ID, periodBytes, reason),
		}).Error; err != nil {
			return err
		}
		out = protocolEndpointUsageResetRecord(reset)
		return nil
	})
	return
}

func (s ProtocolEndpointUsagePeriods) ListProtocolEndpointUsageResets(ctx context.Context, actor, endpointID uint, limit int) ([]network.ProtocolEndpointUsageResetRecord, error) {
	if _, err := providerAdmin(s.DB.WithContext(ctx), actor); err != nil {
		if errors.Is(err, network.ErrProviderPermission) {
			return nil, network.ErrProtocolEndpointUsagePermission
		}
		return nil, err
	}
	var endpoint model.ProtocolEndpoint
	if err := s.DB.WithContext(ctx).Select("id").First(&endpoint, endpointID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, network.ErrProtocolEndpointUsageNotFound
		}
		return nil, err
	}
	var rows []model.ProtocolEndpointUsageReset
	if err := s.DB.WithContext(ctx).Where("protocol_endpoint_id = ?", endpointID).Order("id desc").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]network.ProtocolEndpointUsageResetRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, protocolEndpointUsageResetRecord(row))
	}
	return result, nil
}

func protocolEndpointUsageResetRecord(row model.ProtocolEndpointUsageReset) network.ProtocolEndpointUsageResetRecord {
	return network.ProtocolEndpointUsageResetRecord{ID: row.ID, ProtocolEndpointID: row.ProtocolEndpointID, ActorUserID: row.ActorUserID, ResetAt: row.ResetAt, PeriodUsedBytes: row.PeriodUsedBytes, Reason: row.Reason}
}
