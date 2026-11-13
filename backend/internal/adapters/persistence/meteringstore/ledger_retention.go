package meteringstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/entitlements"
	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const LedgerMinuteHorizon = metering.MinuteDetailHorizon
const LedgerHourHorizon = metering.HourlyHistoryHorizon
const LedgerDayHorizon = metering.DailyHistoryHorizon

type LedgerRetention struct {
	DB  *gorm.DB
	Now func() time.Time
}

func (s LedgerRetention) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func (s LedgerRetention) HasPending(ctx context.Context) (bool, error) {
	var p model.MeteringLedgerRetentionProgress
	err := s.DB.WithContext(ctx).First(&p, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, nil
	}
	return p.AfterID < p.UpperID || !p.NextPassAt.After(s.now()), err
}
func (s LedgerRetention) Drain(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 200 {
		return 0, errors.New("ledger retention accepts 1..200 sources")
	}
	now := s.now().Truncate(time.Millisecond)
	done := 0
	var heldNotice error
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var p model.MeteringLedgerRetentionProgress
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&p, 1).Error; err != nil {
			return err
		}
		if err := retireChartHistory(tx, now, limit); err != nil {
			return err
		}
		if p.AfterID >= p.UpperID {
			if p.NextPassAt.After(now) {
				return nil
			}
			var last struct{ ID uint }
			if err := tx.Model(&model.TrafficRecord{}).Select("id").Order("id DESC").Limit(1).Find(&last).Error; err != nil {
				return err
			}
			p.AfterID = 0
			p.HeldCount, p.LastHeldID, p.LastHoldReason = 0, 0, ""
			p.UpperID = last.ID
			p.HighWaterID = max(p.HighWaterID, p.UpperID)
			// The fixed progress row serializes background passes only; accounting
			// writers never acquire this row.
			r := tx.Model(&model.MeteringLedgerRetentionProgress{}).Where("id=1 AND after_id>=upper_id AND next_pass_at<=?", now).Updates(map[string]any{"after_id": 0, "upper_id": p.UpperID})
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != 1 {
				return nil
			}
		}
		var candidates []model.TrafficRecord
		if err := tx.Select("id,node_id").Where("id>? AND id<=?", p.AfterID, p.UpperID).Order("id").Limit(limit).Find(&candidates).Error; err != nil {
			return err
		}
		for _, candidate := range candidates {
			if err := ctx.Err(); err != nil {
				return err
			}
			changed, err := s.retireOne(tx, candidate, now)
			if err != nil {
				var hold *ledgerRetentionHold
				if !errors.As(err, &hold) {
					return err
				}
				p.HeldCount++
				p.LastHeldID = candidate.ID
				p.LastHoldReason = hold.reason
			}
			if changed {
				done++
			}
			p.AfterID = candidate.ID
		}
		if len(candidates) < limit {
			p.AfterID = p.UpperID
			p.NextPassAt = now.Add(time.Hour)
			if p.HeldCount > 0 {
				heldNotice = fmt.Errorf("%d 条旧流量明细暂保留；记录 #%d：%s", p.HeldCount, p.LastHeldID, p.LastHoldReason)
			}
		}
		return tx.Model(&model.MeteringLedgerRetentionProgress{}).Where("id=1").Updates(map[string]any{"after_id": p.AfterID, "upper_id": p.UpperID, "next_pass_at": p.NextPassAt, "held_count": p.HeldCount, "last_held_id": p.LastHeldID, "last_hold_reason": p.LastHoldReason, "high_water_id": p.HighWaterID}).Error
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	return done, heldNotice
}

type retainedDetail struct {
	Kind           string    `json:"kind"`
	Version        int       `json:"version"`
	Level          int16     `json:"level"`
	ReceiveThrough time.Time `json:"receive_through"`
	QuotaCut       time.Time `json:"quota_cut"`
	AfterCut       bool      `json:"after_cut"`
}

// Future scheduled reset cutoffs are after this compaction pass. Preserve the
// current exact boundary inside historical receive buckets; never round quota.
func currentQuotaCut(tx *gorm.DB, r model.TrafficRecord, now time.Time) (time.Time, error) {
	var sub model.Subscription
	err := tx.Select("id,start_at,reset_policy,entitlement_epoch").Clauses(clause.Locking{Strength: "SHARE", Options: "NOWAIT"}).First(&sub, r.SubscriptionID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	if sub.EntitlementEpoch != r.EntitlementEpoch {
		return time.Time{}, nil
	}
	if sub.ResetPolicy < 1 || sub.ResetPolicy > 4 {
		return time.Time{}, nil
	}
	a := sub.StartAt.UTC()
	cut := a
	switch sub.ResetPolicy {
	case 1:
		cut = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	case 2:
		months := (now.Year()-a.Year())*12 + int(now.Month()-a.Month())
		cut = entitlements.AddCalendarMonths(a, max(0, months))
		if cut.After(now) {
			cut = entitlements.AddCalendarMonths(a, max(0, months-1))
		}
	case 3:
		cut = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	case 4:
		years := max(0, now.Year()-a.Year())
		cut = entitlements.AddCalendarMonths(a, years*12)
		if cut.After(now) {
			cut = entitlements.AddCalendarMonths(a, max(0, years-1)*12)
		}
	}
	if cut.Before(a) {
		cut = a
	}
	return cut, nil
}
func retentionLevel(r model.TrafficRecord, through, now time.Time) int16 {
	// Receipt time controls financial detail lifetime. Old reports received today
	// remain valid hot contributions even though occurrence time is historical.
	age := now.Sub(through)
	if age >= LedgerDayHorizon {
		return 3
	}
	if age >= LedgerHourHorizon {
		return 2
	}
	if age >= LedgerMinuteHorizon {
		return 1
	}
	return 0
}
func (s LedgerRetention) retireOne(tx *gorm.DB, candidate model.TrafficRecord, now time.Time) (bool, error) {
	// Source writers own nodes before source records. Skip busy parents instead
	// of holding a ledger lock while an administrator waits for that ledger.
	var node struct{ ID uint }
	q := tx.Model(&model.Node{}).Select("id").Clauses(clause.Locking{Strength: "SHARE", Options: "SKIP LOCKED"}).Where("id=?", candidate.NodeID).Find(&node)
	if q.Error != nil {
		return false, q.Error
	}
	if q.RowsAffected == 0 {
		var n int64
		if err := tx.Model(&model.Node{}).Where("id=?", candidate.NodeID).Count(&n).Error; err != nil {
			return false, err
		}
		if n > 0 {
			return false, nil
		}
	}
	var r model.TrafficRecord
	columns := "id,retention_level,result_recorded,entitlement_epoch,user_id,subscription_id,node_id,protocol_endpoint_id,report_id,flow_id,event_type,event_revision,nonce,raw_bytes,upload_bytes,download_bytes,traffic_calc_mode,protocol_multiplier_milli,used_bytes,record_at,created_at,updated_at,is_aggregate,record_count,substr(meta,1,257) AS meta"
	q = tx.Select(columns).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", candidate.ID).Find(&r)
	if q.Error != nil {
		return false, q.Error
	}
	if q.RowsAffected == 0 {
		return false, nil
	}
	if !r.ResultRecorded || r.CreatedAt.IsZero() || r.UpdatedAt.After(now.Add(-LedgerMinuteHorizon)) {
		return false, nil
	}
	through := r.CreatedAt
	if r.RetentionLevel == 0 && r.IsAggregate && r.UpdatedAt.After(through) {
		through = r.UpdatedAt
	}
	if r.RetentionLevel > 0 {
		var info retainedDetail
		if !validAggregateRecord(r) || json.Unmarshal([]byte(r.Meta), &info) != nil || info.Kind != "retained_settlement" || info.Version != 1 || info.Level != r.RetentionLevel {
			return false, &ledgerRetentionHold{"汇总元数据未通过校验"}
		}
		through = info.ReceiveThrough
	}
	level := retentionLevel(r, through, now)
	if level <= r.RetentionLevel {
		return false, nil
	}
	if r.RetentionLevel == 0 && r.IsAggregate && !validAggregateRecord(r) {
		return false, &ledgerRetentionHold{"原生计费来源格式未通过校验"}
	}
	if r.RawBytes < 0 || r.UploadBytes < 0 || r.DownloadBytes < 0 || r.UsedBytes < 0 || r.RecordCount < 1 {
		return false, &ledgerRetentionHold{"原始计量数值无效"}
	}
	if r.IsAggregate && r.RetentionLevel == 0 {
		var proof minuteSettlementAudit
		if json.Unmarshal([]byte(r.Meta), &proof) != nil || proof.Kind != "minute_settlement" || proof.Version != 1 || proof.AccountingSamples < uint64(r.RecordCount) || (proof.AccountingSamples <= math.MaxUint64/999 && proof.RoundingNumerator > proof.AccountingSamples*999) {
			return false, &ledgerRetentionHold{"分钟计费证明未通过校验"}
		}
	}
	// Projectors must finish original contributions before their source changes.
	var pending struct{ TrafficRecordID uint }
	q = tx.Model(&model.MeteringUsageProjectionPending{}).Select("traffic_record_id").Where("traffic_record_id=?", r.ID).Limit(1).Find(&pending)
	if q.Error != nil || q.RowsAffected > 0 {
		return false, q.Error
	}
	var diagnostic model.TrafficDiagnosticMaterial
	q = tx.Select("traffic_record_id,retired_at").Where("traffic_record_id=?", r.ID).Limit(1).Find(&diagnostic)
	if q.Error != nil {
		return false, q.Error
	}
	if q.RowsAffected > 0 && diagnostic.RetiredAt == nil {
		return false, nil
	}
	if r.EventType != "" && !r.IsAggregate {
		proof, found, err := findProofArchiveHeader(tx, r)
		if err != nil {
			if errors.Is(err, metering.ErrProofArchive) {
				return false, &ledgerRetentionHold{"归档计费证明损坏或缺失"}
			}
			return false, err
		}
		if !found && (r.EventType == "flow.updated" || r.EventType == "flow.completed") {
			var body struct{ Meta string }
			if e := tx.Model(&model.TrafficRecord{}).Select("substr(meta,1,?) AS meta", proofArchiveBodyLimit+1).Where("id=?", r.ID).Take(&body).Error; e != nil {
				return false, e
			}
			if len(body.Meta) > proofArchiveBodyLimit || !knownNativeProof(r, body.Meta) {
				return false, &ledgerRetentionHold{"旧原生计费证明尚未完成校验"}
			}
		}
		if found {
			budget := proofArchiveReadBudget
			_, body, e := readProofArchivePayload(tx, proof, &budget)
			if e != nil {
				if errors.Is(e, metering.ErrProofArchive) {
					return false, &ledgerRetentionHold{"归档计费证明损坏或缺失"}
				}
				return false, e
			}
			if !knownNativeProof(r, body) {
				return false, &ledgerRetentionHold{"归档计费证明尚未完成校验"}
			}
		}
	}
	var result model.MeteringSettlementResult
	key := settlementResultKey(r)
	// Results only increase. A conservative snapshot can defer retirement; it
	// must not hold a result lock while inserting a destination ledger bucket.
	if err := tx.Where("result_key=?", key).First(&result).Error; err != nil {
		return false, err
	}
	if result.UsedBytes < r.UsedBytes || result.RawBytes < r.RawBytes || result.UploadBytes < r.UploadBytes || result.DownloadBytes < r.DownloadBytes || result.RecordCount < r.RecordCount {
		return false, &ledgerRetentionHold{"结算结果未覆盖原始明细"}
	}
	cut, err := currentQuotaCut(tx, r, now)
	if err != nil {
		return false, err
	}
	if level == 3 && !cut.IsZero() && !through.Before(cut) {
		level = 2
		if level <= r.RetentionLevel {
			return false, nil
		}
	}
	if !cut.IsZero() && r.CreatedAt.Before(cut) && !through.Before(cut) {
		return false, nil
	}
	if err := retainLegacyReportIdentity(tx, r); err != nil {
		return false, err
	}
	target := uint(0)
	if level < 3 {
		width := time.Hour
		if level == 2 {
			width = 24 * time.Hour
		}
		at := r.At.UTC().Truncate(width)
		received := r.CreatedAt.UTC().Truncate(width)
		after := !cut.IsZero() && !r.CreatedAt.Before(cut)
		raw, _ := json.Marshal([]any{key, level, at, received, cut, after})
		h := sha256.Sum256(raw)
		report := retainedSettlementPrefix + hex.EncodeToString(h[:])
		summary := model.TrafficRecord{ResultRecorded: true, RetentionLevel: level, EntitlementEpoch: r.EntitlementEpoch, UserID: r.UserID, SubscriptionID: r.SubscriptionID, NodeID: r.NodeID, ProtocolEndpointID: r.ProtocolEndpointID, TrafficCalcMode: r.TrafficCalcMode, ProtocolMultiplierMilli: r.ProtocolMultiplierMilli, ReportID: report, Nonce: completionNonce(report), EventType: retainedSettlementType, IsAggregate: true, At: at, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, RecordCount: 0}
		summary.Meta = string(mustJSON(retainedDetail{Kind: "retained_settlement", Version: 1, Level: level, ReceiveThrough: through, QuotaCut: cut, AfterCut: after}))
		seed := map[string]any{"result_recorded": true, "retention_level": summary.RetentionLevel, "entitlement_epoch": summary.EntitlementEpoch, "user_id": summary.UserID, "subscription_id": summary.SubscriptionID, "node_id": summary.NodeID, "protocol_endpoint_id": summary.ProtocolEndpointID, "traffic_calc_mode": summary.TrafficCalcMode, "protocol_multiplier_milli": summary.ProtocolMultiplierMilli, "report_id": summary.ReportID, "nonce": summary.Nonce, "event_type": summary.EventType, "is_aggregate": true, "record_at": summary.At, "created_at": summary.CreatedAt, "updated_at": summary.UpdatedAt, "record_count": int64(0), "raw_bytes": int64(0), "upload_bytes": int64(0), "download_bytes": int64(0), "used_bytes": int64(0), "meta": summary.Meta}
		if err := tx.Table("traffic_records").Clauses(clause.OnConflict{DoUpdates: clause.Assignments(map[string]any{"id": gorm.Expr("id")})}).Create(seed).Error; err != nil {
			return false, err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id=? AND report_id=?", r.NodeID, report).First(&summary).Error; err != nil {
			return false, err
		}
		var info retainedDetail
		if json.Unmarshal([]byte(summary.Meta), &info) != nil || info.Kind != "retained_settlement" || info.Level != level || !summary.ResultRecorded || !validAggregateRecord(summary) {
			return false, errors.New("retention target changed")
		}

		for _, p := range [][2]*int64{{&summary.RawBytes, &r.RawBytes}, {&summary.UploadBytes, &r.UploadBytes}, {&summary.DownloadBytes, &r.DownloadBytes}, {&summary.UsedBytes, &r.UsedBytes}, {&summary.RecordCount, &r.RecordCount}} {
			v, e := checkedProjectionSum(*p[0], *p[1])
			if e != nil {
				return false, e
			}
			*p[0] = v
		}
		if r.CreatedAt.Before(summary.CreatedAt) {
			summary.CreatedAt = r.CreatedAt
		}
		if r.UpdatedAt.After(summary.UpdatedAt) {
			summary.UpdatedAt = r.UpdatedAt
		}
		if through.After(info.ReceiveThrough) {
			info.ReceiveThrough = through
		}
		summary.Meta = string(mustJSON(info))
		if err := tx.Model(&summary).Select("raw_bytes", "upload_bytes", "download_bytes", "used_bytes", "record_count", "created_at", "updated_at", "meta").UpdateColumns(&summary).Error; err != nil {
			return false, err
		}
		target = summary.ID
	}
	link := model.MeteringLedgerTarget{SourceID: r.ID, NodeID: r.NodeID, ProtocolEndpointID: r.ProtocolEndpointID, RecordID: target}
	if level == 3 {
		link.ResultID = result.ID
	}
	if r.NodeID != 0 && r.ReportID != "" {
		if err := tx.Create(&link).Error; err != nil {
			return false, err
		}

	}
	// These are expired optional bodies. Numeric financial results and exact
	// report/nonce identities have already transferred in this same transaction.
	if err := tx.Where("traffic_record_id=?", r.ID).Delete(&model.TrafficDiagnosticMaterial{}).Error; err != nil {
		return false, err
	}
	if err := tx.Where("traffic_record_id=?", r.ID).Delete(&model.TrafficMetaArchive{}).Error; err != nil {
		return false, err
	}
	if err := tx.Where("id=?", r.ID).Delete(&model.TrafficRecord{}).Error; err != nil {
		return false, err
	}
	return true, nil
}
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

type ledgerRetentionHold struct{ reason string }

func (e *ledgerRetentionHold) Error() string { return e.reason }
