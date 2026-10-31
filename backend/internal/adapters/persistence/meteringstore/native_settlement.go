package meteringstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const nativeSettlementPrefix = "bucket:v1:"
const nativeSettlementType = "settlement.minute"

type minuteSettlementAudit struct {
	Kind              string `json:"kind"`
	Version           int    `json:"version"`
	AccountingSamples uint64 `json:"accounting_samples"`
	RoundingNumerator uint64 `json:"rounding_numerator"`
}

func nativeSettlementAudit(record model.TrafficRecord) (minuteSettlementAudit, error) {
	audit := minuteSettlementAudit{Kind: "minute_settlement", Version: 1}
	// These two numeric proof fields describe pre-cap rounding, independent of
	// the original event body. Unknown diagnostic fields are never retained.
	if record.Meta != "" {
		if err := json.Unmarshal([]byte(record.Meta), &audit); err != nil {
			return audit, errors.New("native accounting proof is not valid JSON")
		}
	}
	audit.Kind = "minute_settlement"
	audit.Version = 1
	if audit.AccountingSamples == 0 {
		audit.AccountingSamples = uint64(trafficRecordCount(record))
		if record.RawBytes > 0 && record.ProtocolMultiplierMilli > 0 {
			audit.RoundingNumerator = uint64((1000 - (record.RawBytes%1000)*(record.ProtocolMultiplierMilli%1000)%1000) % 1000)
		}
	}
	return audit, nil
}

func trafficRecordCount(r model.TrafficRecord) int64 {
	if r.RecordCount > 0 {
		return r.RecordCount
	}
	return 1
}

// Amounts are the ALREADY calculated per-report deltas, including multiplier
// rounding and quota caps. Never calculate a new charge from aggregate raw bytes.
// Created-minute partitions also split at the subscription anchor's sub-minute
// offset, so anniversary reset cutoffs can never straddle a stored contribution.
func prepareNativeSettlement(r model.TrafficRecord, anchor time.Time) (model.TrafficRecord, error) {
	if r.NodeID == 0 || r.SubscriptionID == 0 || r.ProtocolEndpointID == 0 || r.ReportID == "" || strings.HasPrefix(r.ReportID, nativeSettlementPrefix) {
		return model.TrafficRecord{}, errors.New("invalid native settlement identity")
	}
	if r.RawBytes < 0 || r.UploadBytes < 0 || r.DownloadBytes < 0 || r.UsedBytes < 0 {
		return model.TrafficRecord{}, errors.New("negative native settlement delta")
	}
	now := r.CreatedAt.UTC()
	if now.IsZero() {
		return model.TrafficRecord{}, errors.New("native settlement receive clock missing")
	}
	at := r.At.UTC()
	if at.IsZero() {
		at = now
	}
	offset := time.Duration(anchor.Second())*time.Second + time.Duration(anchor.Nanosecond())
	key, _ := json.Marshal([]any{r.UserID, r.SubscriptionID, r.EntitlementEpoch, r.NodeID, r.ProtocolEndpointID, r.TrafficCalcMode, r.ProtocolMultiplierMilli, at.Truncate(time.Minute).Format(time.RFC3339Nano), now.Truncate(time.Minute).Format(time.RFC3339Nano), int64(offset), now.Sub(now.Truncate(time.Minute)) >= offset})
	digest := sha256.Sum256(key)
	report := nativeSettlementPrefix + hex.EncodeToString(digest[:])
	audit, err := nativeSettlementAudit(r)
	if err != nil {
		return model.TrafficRecord{}, err
	}
	meta, _ := json.Marshal(audit)
	return model.TrafficRecord{UserID: r.UserID, SubscriptionID: r.SubscriptionID, EntitlementEpoch: r.EntitlementEpoch, NodeID: r.NodeID, ProtocolEndpointID: r.ProtocolEndpointID, ReportID: report, Nonce: completionNonce(report), EventType: nativeSettlementType, RawBytes: r.RawBytes, UploadBytes: r.UploadBytes, DownloadBytes: r.DownloadBytes, UsedBytes: r.UsedBytes, TrafficCalcMode: r.TrafficCalcMode, ProtocolMultiplierMilli: r.ProtocolMultiplierMilli, At: at.Truncate(time.Minute), CreatedAt: now, UpdatedAt: now, Meta: string(meta), IsAggregate: true, RecordCount: trafficRecordCount(r)}, nil
}
func persistNativeSettlement(tx *gorm.DB, row *model.TrafficRecord) error {
	incoming := func(column string) string {
		if tx.Dialector.Name() == "sqlite" {
			return "excluded." + column
		}
		return "VALUES(" + column + ")"
	}
	var audit minuteSettlementAudit
	if err := json.Unmarshal([]byte(row.Meta), &audit); err != nil {
		return err
	}
	updates := map[string]any{"updated_at": row.UpdatedAt}
	// Both databases keep the same fixed-size arithmetic proof. No per-event
	// runtime, credential, flow body or timestamp list survives in this JSON.
	updates["meta"] = gorm.Expr("JSON_OBJECT('kind','minute_settlement','version',1,'accounting_samples',COALESCE(JSON_EXTRACT(meta,'$.accounting_samples'),0)+?,'rounding_numerator',COALESCE(JSON_EXTRACT(meta,'$.rounding_numerator'),0)+?)", audit.AccountingSamples, audit.RoundingNumerator)
	for _, column := range []string{"raw_bytes", "upload_bytes", "download_bytes", "used_bytes", "record_count"} {
		updates[column] = gorm.Expr(column + " + " + incoming(column))
	}
	if tx.Dialector.Name() == "mysql" {
		updates["id"] = gorm.Expr("LAST_INSERT_ID(id)")
	}
	if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "node_id"}, {Name: "report_id"}}, DoUpdates: clause.Assignments(updates)}).Create(row).Error; err != nil {
		return err
	}
	if row.ID == 0 {
		var stored struct{ ID uint }
		if err := tx.Model(&model.TrafficRecord{}).Select("id").Where("node_id = ? AND report_id = ?", row.NodeID, row.ReportID).Take(&stored).Error; err != nil {
			return err
		}
		row.ID = stored.ID
	}
	return nil
}
func storeNativeSettlement(tx *gorm.DB, r *model.TrafficRecord, anchor time.Time) error {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = tx.NowFunc().UTC()
	}
	if tx.Dialector.Name() == "mysql" {
		r.CreatedAt = r.CreatedAt.UTC().Truncate(time.Millisecond)
	}
	row, err := prepareNativeSettlement(*r, anchor)
	if err != nil {
		return err
	}
	if err := persistNativeSettlement(tx, &row); err != nil {
		return err
	}
	r.ID = row.ID
	r.IsAggregate = true
	r.RecordCount = trafficRecordCount(*r)
	r.CreatedAt = row.CreatedAt
	return nil
}

// SQLite owns the enclosing writer slot; coalesce the entire accepted batch
// before touching indexes. MySQL retains immediate database-collation identity
// admission, while the same canonical writer stores its individual deltas.
func storeNativeSettlementBatch(tx *gorm.DB, records []model.TrafficRecord, anchors map[uint]time.Time) error {
	type group struct {
		row     model.TrafficRecord
		indices []int
	}
	groups := map[string]*group{}
	for i, r := range records {
		if r.CreatedAt.IsZero() {
			r.CreatedAt = tx.NowFunc().UTC()
		}
		row, err := prepareNativeSettlement(r, anchors[r.SubscriptionID])
		if err != nil {
			return err
		}
		g := groups[row.ReportID]
		if g == nil {
			groups[row.ReportID] = &group{row: row, indices: []int{i}}
			continue
		}
		for _, pair := range [][2]*int64{{&g.row.RawBytes, &row.RawBytes}, {&g.row.UploadBytes, &row.UploadBytes}, {&g.row.DownloadBytes, &row.DownloadBytes}, {&g.row.UsedBytes, &row.UsedBytes}, {&g.row.RecordCount, &row.RecordCount}} {
			sum, err := checkedProjectionSum(*pair[0], *pair[1])
			if err != nil {
				return err
			}
			*pair[0] = sum
		}
		var oldAudit, newAudit minuteSettlementAudit
		if err := json.Unmarshal([]byte(g.row.Meta), &oldAudit); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(row.Meta), &newAudit); err != nil {
			return err
		}
		if oldAudit.AccountingSamples > math.MaxInt64-newAudit.AccountingSamples || oldAudit.RoundingNumerator > math.MaxInt64-newAudit.RoundingNumerator {
			return errors.New("minute accounting proof aggregate overflow")
		}
		oldAudit.AccountingSamples += newAudit.AccountingSamples
		oldAudit.RoundingNumerator += newAudit.RoundingNumerator
		encoded, _ := json.Marshal(oldAudit)
		g.row.Meta = string(encoded)
		g.indices = append(g.indices, i)
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		g := groups[key]
		if err := persistNativeSettlement(tx, &g.row); err != nil {
			return err
		}
		for _, i := range g.indices {
			records[i].ID = g.row.ID
			records[i].IsAggregate = true
			records[i].RecordCount = trafficRecordCount(records[i])
			records[i].CreatedAt = g.row.CreatedAt
		}
	}
	return nil
}

func checkedProjectionSum(a, b int64) (int64, error) {
	if a < 0 || b < 0 || a > math.MaxInt64-b {
		return 0, errors.New("native settlement aggregate exceeds exact byte/count range")
	}
	return a + b, nil
}
