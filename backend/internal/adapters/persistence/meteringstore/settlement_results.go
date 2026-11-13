package meteringstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Result writes happen after source writes and before the source transaction's
// ACK. The prefix progress and its totals always commit together.
func addSettlementResults(tx *gorm.DB, records []model.TrafficRecord, backfill bool) error {
	var progress model.MeteringSettlementProgress
	// UpperID is immutable; source coverage is re-read under the source lock.
	// Never queue live admission behind the maintenance progress owner.
	if err := tx.First(&progress, 1).Error; err != nil {
		return err
	}

	ids := make([]uint, 0, len(records))
	for _, r := range records {
		if r.ID == 0 {
			return errors.New("settlement result requires source identity")
		}
		ids = append(ids, r.ID)
	}
	var storedSources []model.TrafficRecord
	if len(ids) > 0 {
		if err := tx.Select("id,updated_at,result_recorded,user_id,subscription_id,entitlement_epoch,node_id,protocol_endpoint_id,traffic_calc_mode,protocol_multiplier_milli,raw_bytes,upload_bytes,download_bytes,used_bytes,record_count").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", ids).Order("id").Find(&storedSources).Error; err != nil {
			return err
		}
	}
	sources := map[uint]model.TrafficRecord{}
	for _, r := range storedSources {
		sources[r.ID] = r
	}
	fullSeen := map[uint]bool{}
	covered := []uint{}
	groups := map[string]*model.MeteringSettlementResult{}
	for _, r := range records {
		source, ok := sources[r.ID]
		if !ok {
			return errors.New("settlement result source disappeared")
		}
		if r.UserID != source.UserID || r.SubscriptionID != source.SubscriptionID || r.EntitlementEpoch != source.EntitlementEpoch || r.NodeID != source.NodeID || r.ProtocolEndpointID != source.ProtocolEndpointID || r.TrafficCalcMode != source.TrafficCalcMode || r.ProtocolMultiplierMilli != source.ProtocolMultiplierMilli {
			return errors.New("settlement contribution scope differs from source")
		}
		if r.RawBytes < 0 || r.UploadBytes < 0 || r.DownloadBytes < 0 || r.UsedBytes < 0 || r.RawBytes > source.RawBytes || r.UploadBytes > source.UploadBytes || r.DownloadBytes > source.DownloadBytes || r.UsedBytes > source.UsedBytes {
			return errors.New("settlement contribution exceeds source")
		}
		if backfill && source.ResultRecorded {
			continue
		}
		if !source.ResultRecorded {
			if !backfill && r.ID > progress.AfterID && r.ID <= progress.UpperID {
				continue
			}
			if fullSeen[r.ID] {
				continue
			}
			fullSeen[r.ID] = true
			r = source
			covered = append(covered, r.ID)
		}
		key := settlementResultKey(r)
		row := groups[key]
		if row == nil {
			row = &model.MeteringSettlementResult{Key: key, UserID: r.UserID, SubscriptionID: r.SubscriptionID, EntitlementEpoch: r.EntitlementEpoch, NodeID: r.NodeID, ProtocolEndpointID: r.ProtocolEndpointID, TrafficCalcMode: r.TrafficCalcMode, ProtocolMultiplierMilli: r.ProtocolMultiplierMilli}
			groups[key] = row
		}
		for _, pair := range [][2]*int64{{&row.RawBytes, &r.RawBytes}, {&row.UploadBytes, &r.UploadBytes}, {&row.DownloadBytes, &r.DownloadBytes}, {&row.UsedBytes, &r.UsedBytes}} {
			v, e := checkedProjectionSum(*pair[0], *pair[1])
			if e != nil {
				return e
			}
			*pair[0] = v
		}
		v, e := checkedProjectionSum(row.RecordCount, trafficRecordCount(r))
		if e != nil {
			return e
		}
		row.RecordCount = v
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		incoming := *groups[key]
		empty := incoming
		empty.RawBytes = 0
		empty.UploadBytes = 0
		empty.DownloadBytes = 0
		empty.UsedBytes = 0
		empty.RecordCount = 0
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&empty).Error; err != nil {
			return err
		}
		var stored model.MeteringSettlementResult
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&stored, "result_key = ?", key).Error; err != nil {
			return err
		}
		if stored.UserID != incoming.UserID || stored.SubscriptionID != incoming.SubscriptionID || stored.EntitlementEpoch != incoming.EntitlementEpoch || stored.NodeID != incoming.NodeID || stored.ProtocolEndpointID != incoming.ProtocolEndpointID || stored.TrafficCalcMode != incoming.TrafficCalcMode || stored.ProtocolMultiplierMilli != incoming.ProtocolMultiplierMilli {
			return errors.New("settlement result scope changed")
		}
		for _, pair := range [][2]*int64{{&stored.RawBytes, &incoming.RawBytes}, {&stored.UploadBytes, &incoming.UploadBytes}, {&stored.DownloadBytes, &incoming.DownloadBytes}, {&stored.UsedBytes, &incoming.UsedBytes}, {&stored.RecordCount, &incoming.RecordCount}} {
			v, e := checkedProjectionSum(*pair[0], *pair[1])
			if e != nil {
				return e
			}
			*pair[0] = v
		}
		if err := tx.Model(&stored).Select("raw_bytes", "upload_bytes", "download_bytes", "used_bytes", "record_count").Updates(&stored).Error; err != nil {
			return err
		}
	}
	if len(covered) > 0 {
		// MySQL's source column also has ON UPDATE CURRENT_TIMESTAMP. Explicitly
		// preserve each original receive/proof timestamp when changing coverage.
		parts := []string{"CASE id"}
		args := []any{}
		for _, id := range covered {
			parts = append(parts, "WHEN ? THEN ?")
			args = append(args, id, sources[id].UpdatedAt)
		}
		parts = append(parts, "ELSE updated_at END")
		if err := tx.Model(&model.TrafficRecord{}).Where("id IN ?", covered).UpdateColumns(map[string]any{"result_recorded": true, "updated_at": gorm.Expr(strings.Join(parts, " "), args...)}).Error; err != nil {
			return err
		}
	}

	return nil
}

type SettlementResults struct{ DB *gorm.DB }

func (s SettlementResults) HasPending(ctx context.Context) (bool, error) {
	var p model.MeteringSettlementProgress
	err := s.DB.WithContext(ctx).First(&p, 1).Error
	return p.AfterID < p.UpperID, err
}
func (s SettlementResults) Drain(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 200 {
		return 0, errors.New("settlement result batch must be 1..200")
	}
	done := 0
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var initial model.MeteringSettlementProgress
		if err := tx.First(&initial, 1).Error; err != nil {
			return err
		}
		if initial.AfterID >= initial.UpperID {
			return nil
		}
		var rows []model.TrafficRecord
		// Lock sources before progress, matching the live authority writer.
		columns := "id,updated_at,result_recorded,user_id,subscription_id,entitlement_epoch,node_id,protocol_endpoint_id,traffic_calc_mode,protocol_multiplier_milli,raw_bytes,upload_bytes,download_bytes,used_bytes,record_count"
		if err := tx.Select(columns).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id > ? AND id <= ?", initial.AfterID, initial.UpperID).Order("id").Limit(limit).Find(&rows).Error; err != nil {
			return err
		}
		var current model.MeteringSettlementProgress
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, 1).Error; err != nil {
			return err
		}
		// Another worker may have covered this prefix while sources were waiting.
		if current.AfterID != initial.AfterID || current.UpperID != initial.UpperID {
			return nil
		}
		if err := addSettlementResults(tx, rows, true); err != nil {
			return err
		}
		after := current.UpperID
		if len(rows) == limit {
			after = rows[len(rows)-1].ID
		}
		if err := tx.Model(&current).Update("after_id", after).Error; err != nil {
			return err
		}
		done = len(rows)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	return done, err
}

// A read snapshot sees covered results plus only the unconverted old prefix.
// Live contributions to that prefix are intentionally left in the source until
// transfer; contributions elsewhere are already included in the results.
func settlementResultSource(db *gorm.DB) *gorm.DB {
	columns := "user_id,subscription_id,entitlement_epoch,node_id,protocol_endpoint_id,traffic_calc_mode,protocol_multiplier_milli,raw_bytes,upload_bytes,download_bytes,used_bytes,record_count"
	results := db.Session(&gorm.Session{NewDB: true}).Table("metering_settlement_results").Select(columns)
	pending := db.Session(&gorm.Session{NewDB: true}).Table("traffic_records").Select(columns).Where("result_recorded = ?", false)
	return db.Session(&gorm.Session{NewDB: true}).Table("(? UNION ALL ?) AS settlement_result_source", results, pending)
}

func settlementResultKey(r model.TrafficRecord) string {
	raw, _ := json.Marshal([]any{r.UserID, r.SubscriptionID, r.EntitlementEpoch, r.NodeID, r.ProtocolEndpointID, r.TrafficCalcMode, r.ProtocolMultiplierMilli})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
