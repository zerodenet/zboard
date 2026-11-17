package meteringstore

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ProofArchives struct {
	DB  *gorm.DB
	Now func() time.Time
}

var proofFactColumns = []string{"id", "entitlement_epoch", "user_id", "subscription_id", "node_id", "protocol_endpoint_id", "report_id", "flow_id", "event_type", "event_revision", "nonce", "raw_bytes", "upload_bytes", "download_bytes", "traffic_calc_mode", "protocol_multiplier_milli", "used_bytes", "record_at", "created_at", "updated_at"}

func (s ProofArchives) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC().Truncate(time.Millisecond)
	}
	return time.Now().UTC().Truncate(time.Millisecond)
}
func proofArchiveTxOptions(db *gorm.DB) *sql.TxOptions {
	o := &sql.TxOptions{}
	if db.Dialector.Name() == "mysql" {
		o.Isolation = sql.LevelReadCommitted
	}
	return o
}

func (s ProofArchives) Archive(ctx context.Context, q metering.ProofArchiveRequest) (metering.ProofArchiveResult, error) {
	var out metering.ProofArchiveResult
	if s.DB == nil || ctx == nil || q.Limit < 1 || q.Limit > 32 || q.ThroughID == 0 || q.AfterID > q.ThroughID || q.Before.IsZero() || q.Before.After(s.now().Add(-7*24*time.Hour)) {
		return out, errors.New("proof archive requires a batch of 1..32, explicit ID range and boundary at least seven days old")
	}
	if err := datastore.ValidateTrafficMetaArchiveDatabase(s.DB.WithContext(ctx)); err != nil {
		return out, err
	}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ids []uint
		if err := tx.Model(&model.TrafficRecord{}).Where("id > ? AND id <= ?", q.AfterID, q.ThroughID).Order("id").Limit(q.Limit).Pluck("id", &ids).Error; err != nil {
			return err
		}
		out.NextAfterID = q.AfterID
		budget := proofArchiveReadBudget
		for _, id := range ids {
			if err := ctx.Err(); err != nil {
				return err
			}
			var source struct {
				model.TrafficRecord `gorm:"embedded"`
				MetaBytes           sql.NullInt64
			}
			columns := append(append([]string{}, proofFactColumns...), "octet_length(meta) AS meta_bytes")
			if err := tx.Model(&model.TrafficRecord{}).Select(columns).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&source).Error; err != nil {
				return err
			}
			if source.MetaBytes.Valid && source.MetaBytes.Int64 > int64(budget) && source.MetaBytes.Int64 <= proofArchiveBodyLimit {
				break
			}
			previousCursor := out.NextAfterID
			out.Scanned++
			out.NextAfterID = id
			if !source.MetaBytes.Valid || source.MetaBytes.Int64 < 1 || source.MetaBytes.Int64 > proofArchiveBodyLimit || source.At.IsZero() || source.At.After(q.Before) || source.CreatedAt.IsZero() || source.UpdatedAt.IsZero() || source.UpdatedAt.Before(source.CreatedAt) || source.CreatedAt.After(q.Before) || source.UpdatedAt.After(q.Before) {
				out.Held++
				continue
			}
			var pending int64
			if err := tx.Model(&model.MeteringUsageProjectionPending{}).Where("traffic_record_id = ?", id).Count(&pending).Error; err != nil {
				return err
			}
			if pending > 0 {
				out.Held++
				continue
			}
			budget -= int(source.MetaBytes.Int64)
			var body struct{ Meta string }
			expr := "substr(CAST(meta AS BLOB),1,?) AS meta"
			if tx.Dialector.Name() == "mysql" {
				expr = "substr(CAST(meta AS BINARY),1,?) AS meta"
			}
			if err := tx.Model(&model.TrafficRecord{}).Select(expr, proofArchiveBodyLimit+1).Where("id = ?", id).Take(&body).Error; err != nil {
				return err
			}
			if int64(len(body.Meta)) != source.MetaBytes.Int64 {
				return metering.ErrProofArchive
			}
			source.Meta = body.Meta
			if source.EventType == "" {
				out.Held++
				continue
			}
			material, err := hydrateProof(tx, source.TrafficRecord, &budget)
			if err != nil {
				if errors.Is(err, errProofArchiveBudget) {
					out.Scanned--
					out.NextAfterID = previousCursor
					break
				}
				return err
			}
			if material.Meta != source.Meta {
				out.AlreadySealed++
				continue
			}
			if !knownNativeProof(source.TrafficRecord, body.Meta) {
				out.Held++
				continue
			}
			var receipt model.MeteringReportReceipt
			e := tx.Where("node_id = ? AND report_id = ?", source.NodeID, source.ReportID).Take(&receipt).Error
			if errors.Is(e, gorm.ErrRecordNotFound) {
				out.Held++
				continue
			}
			if e != nil {
				return e
			}
			if receipt.TrafficRecordID != id || receipt.ProtocolEndpointID != source.ProtocolEndpointID || receipt.ReportID != source.ReportID {
				return ErrReportReceiptConflict
			}
			compressed, err := compressProof(body.Meta)
			if err != nil {
				return err
			}
			marker := archiveReference(proofBodyDigest(body.Meta))
			estimated := uint64(len(compressed) + len(marker) + proofArchiveHeaderAllowance)
			if uint64(len(body.Meta)) <= estimated+proofArchiveMinimumSaving {
				out.NoSaving++
				continue
			}
			// Reserve the stored-payload readback, bounded decompression and
			// final marker readback before creating authority or changing its source.
			if budget < len(compressed)+len(body.Meta)+len(marker)+3 {
				out.Scanned--
				out.NextAfterID = previousCursor
				break
			}
			header := model.TrafficMetaArchive{TrafficRecordID: id, Version: 1, SourceBytes: uint64(len(body.Meta)), SourceSHA256: proofBodyDigest(body.Meta), FactSHA256: proofFactDigest(source.TrafficRecord), CompressedBytes: uint64(len(compressed)), Payload: compressed, CreatedAt: s.now()}
			var old model.TrafficMetaArchive
			found := tx.Select("traffic_record_id").Where("traffic_record_id = ?", id).Take(&old).Error
			if found != nil && !errors.Is(found, gorm.ErrRecordNotFound) {
				return found
			}
			if errors.Is(found, gorm.ErrRecordNotFound) {
				if err := tx.Create(&header).Error; err != nil {
					return err
				}
			}
			// A successful INSERT is not sufficient proof that the stored binary
			// survived driver conversion or a trigger. Verify the durable body in
			// this same transaction before replacing its live reference.
			stored, decoded, err := readProofArchive(tx, source.TrafficRecord, &budget)
			if err != nil || decoded != body.Meta || stored.SourceSHA256 != header.SourceSHA256 {
				return errors.Join(metering.ErrProofArchive, err)
			}
			// Pin the original chronology on MySQL, whose ledger has ON UPDATE.
			changed := tx.Model(&model.TrafficRecord{}).Where("id = ?", id).UpdateColumns(map[string]any{"meta": marker, "updated_at": source.UpdatedAt})
			if changed.Error != nil {
				return changed.Error
			}
			if changed.RowsAffected != 1 {
				return metering.ErrProofArchive
			}
			if err := verifyProofMaterialWrite(tx, source.TrafficRecord, marker, &budget); err != nil {
				return err
			}
			out.Archived++
			out.RawBytes += uint64(len(body.Meta))
			out.EstimatedStoredBytes += estimated
		}
		out.RangeExhausted = out.Scanned == len(ids) && (len(ids) < q.Limit || out.NextAfterID >= q.ThroughID)
		return nil
	}, proofArchiveTxOptions(s.DB))
	if err != nil {
		return metering.ProofArchiveResult{}, err
	}
	return out, nil
}

func (s ProofArchives) Restore(ctx context.Context, id uint) (metering.ProofRestoreResult, error) {
	out := metering.ProofRestoreResult{TrafficRecordID: id}
	if s.DB == nil || ctx == nil || id == 0 {
		return metering.ProofRestoreResult{}, errors.New("proof restore requires one explicit ledger ID")
	}
	if err := datastore.ValidateTrafficMetaArchiveDatabase(s.DB.WithContext(ctx)); err != nil {
		return metering.ProofRestoreResult{}, err
	}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var source struct {
			model.TrafficRecord `gorm:"embedded"`
			MetaBytes           sql.NullInt64
		}
		columns := append(append([]string{}, proofFactColumns...), "octet_length(meta) AS meta_bytes")
		if err := tx.Model(&model.TrafficRecord{}).Select(columns).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&source).Error; err != nil {
			return err
		}
		if !source.MetaBytes.Valid || source.MetaBytes.Int64 < 1 || source.MetaBytes.Int64 > proofArchiveBodyLimit {
			return metering.ErrProofArchive
		}
		var material struct{ Meta string }
		expr := "substr(CAST(meta AS BLOB),1,?) AS meta"
		if tx.Dialector.Name() == "mysql" {
			expr = "substr(CAST(meta AS BINARY),1,?) AS meta"
		}
		if err := tx.Model(&model.TrafficRecord{}).Select(expr, proofArchiveBodyLimit+1).Where("id = ?", id).Take(&material).Error; err != nil {
			return err
		}
		if int64(len(material.Meta)) != source.MetaBytes.Int64 {
			return metering.ErrProofArchive
		}
		record := source.TrafficRecord
		record.Meta = material.Meta
		header, body, err := readProofArchive(tx, record)
		if err != nil {
			return err
		}
		if record.Meta == body {
			return nil
		}
		if record.Meta != archiveReference(header.SourceSHA256) {
			return metering.ErrProofArchive
		}
		changed := tx.Model(&model.TrafficRecord{}).Where("id = ?", id).UpdateColumns(map[string]any{"meta": body, "updated_at": record.UpdatedAt})
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return metering.ErrProofArchive
		}
		if err := verifyProofMaterialWrite(tx, record, body); err != nil {
			return err
		}
		out.Restored = true
		return nil
	}, proofArchiveTxOptions(s.DB))
	if err != nil {
		return metering.ProofRestoreResult{}, err
	}
	return out, nil
}

func verifyProofMaterialWrite(tx *gorm.DB, before model.TrafficRecord, want string, budgets ...*int) error {
	if len(budgets) > 0 && budgets[0] != nil {
		if *budgets[0] < len(want)+1 {
			return errProofArchiveBudget
		}
		*budgets[0] -= len(want) + 1
	}
	var after struct {
		model.TrafficRecord `gorm:"embedded"`
		MetaBytes           uint64
	}
	expr := "substr(CAST(meta AS BLOB),1," + strconv.Itoa(len(want)+1) + ") AS meta"
	if tx.Dialector.Name() == "mysql" {
		expr = "substr(CAST(meta AS BINARY),1," + strconv.Itoa(len(want)+1) + ") AS meta"
	}
	columns := append(append([]string{}, proofFactColumns...), expr, "octet_length(meta) AS meta_bytes")
	if err := tx.Model(&model.TrafficRecord{}).Select(columns).Where("id = ?", before.ID).Take(&after).Error; err != nil {
		return err
	}
	if after.MetaBytes != uint64(len(want)) || after.Meta != want || proofFactDigest(after.TrafficRecord) != proofFactDigest(before) {
		return metering.ErrProofArchive
	}
	return nil
}
