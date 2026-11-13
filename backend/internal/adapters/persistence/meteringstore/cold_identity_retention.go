package meteringstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"slices"
	"strconv"
	"time"
)

type ColdIdentities struct {
	DB  *gorm.DB
	Now func() time.Time
}

func (s ColdIdentities) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return s.DB.NowFunc().UTC()
}
func (s ColdIdentities) HasPending(ctx context.Context) (bool, error) {
	var p []model.MeteringColdProgress
	if e := s.DB.WithContext(ctx).Find(&p).Error; e != nil {
		return false, e
	}
	if len(p) < 4 {
		return true, nil
	}
	for _, r := range p {
		if !r.NextPassAt.After(s.now()) {
			return true, nil
		}
	}
	return false, nil
}

// A pass examines at most 200 sources; page reads have an eight-MiB budget
// and retry with smaller batches. These are maintenance bounds, never report
// admission caps or a maximum report age.
func (s ColdIdentities) Drain(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 200 {
		return 0, errors.New("cold identity batch accepts 1..200 sources")
	}
	examined := 0
	var notices []error
	now := s.now().Truncate(time.Millisecond)
	for kind := uint8(1); kind <= 4; kind++ {
		err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			p := model.MeteringColdProgress{Kind: kind, NextPassAt: time.Unix(0, 0).UTC()}
			if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&p).Error; e != nil {
				return e
			}
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&p, "kind=?", kind).Error; e != nil {
				return e
			}
			if p.NextPassAt.After(now) {
				return nil
			}
			if p.NodeID == 0 && p.AfterID == 0 && p.AfterKey == "" {
				p.HeldCount, p.LastHeldNodeID, p.LastHoldReason = 0, 0, ""
			}
			type candidate struct {
				NodeID uint
				Key    string
				ID     uint
			}
			var rows []candidate
			table, column := "metering_report_receipts", "report_id"
			if kind == coldNonce {
				table, column = "metering_nonce_receipts", "nonce"
			}
			if kind == coldFloor {
				table, column = "flow_cursor_checkpoints", "flow_id"
			}
			if kind == coldTarget {
				if e := tx.Table("metering_ledger_targets").Select("source_id AS id,node_id").Where("source_id>?", p.AfterID).Order("source_id").Limit(limit).Find(&rows).Error; e != nil {
					return e
				}
			} else {
				if e := tx.Table(table).Select("node_id,"+column+" AS `key`").Where("node_id>? OR (node_id=? AND "+column+">?)", p.NodeID, p.NodeID, p.AfterKey).Order("node_id," + column).Limit(limit).Find(&rows).Error; e != nil {
					return e
				}
			}
			keys := make([]string, 0, len(rows))
			for _, r := range rows {
				keys = append(keys, r.Key)
			}
			if e := prepareColdWeights(tx, kind, keys); e != nil {
				return e
			}
			// Queue the short exclusive fence, so continuous report readers cannot
			// starve conversion. Source batches and the job timeout bound ownership.
			// Ascending node locks match ordinary accounting/retention ownership.
			nodes := []uint{}
			seen := map[uint]bool{}
			for _, r := range rows {
				if !seen[r.NodeID] {
					nodes = append(nodes, r.NodeID)
					seen[r.NodeID] = true
				}
			}
			slices.Sort(nodes)
			for _, node := range nodes {
				var n model.Node
				q := tx.Select("id").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", node).Find(&n)
				if q.Error != nil {
					return q.Error
				}
				if q.RowsAffected == 0 {
					var exists int64
					if e := tx.Model(&model.Node{}).Where("id=?", node).Count(&exists).Error; e != nil {
						return e
					}
					if exists > 0 {
						continue
					}
				}
				if e := tx.SavePoint("cold_node").Error; e != nil {
					return e
				}
				nodeErr := func() error {
					byNode := map[uint][]coldIdentity{}
					kept := []candidate{}
					for _, r := range rows {
						if r.NodeID != node {
							continue
						}
						var entry coldIdentity
						switch kind {
						case coldReport:
							var row model.MeteringReportReceipt
							q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id=? AND report_id=?", node, r.Key).Find(&row)
							if q.Error != nil {
								return q.Error
							}
							if q.RowsAffected == 0 || row.CreatedAt.After(now.Add(-LedgerMinuteHorizon)) {
								continue
							}
							// A live legacy source is still a compatibility identity fallback. Seal
							// receipts only after its source has transferred, so backfill cannot
							// recreate the hot claim or lose proof readers' source bindings.
							var link model.MeteringLedgerTarget
							ok, e := loadLedgerTarget(tx, node, row.TrafficRecordID, &link)
							if e != nil {
								return e
							}
							if !ok {
								continue
							}
							entry = coldIdentity{Key: row.ReportID, Numbers: []uint64{uint64(row.ProtocolEndpointID), uint64(row.TrafficRecordID)}}
						case coldNonce:
							var row model.MeteringNonceReceipt
							q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id=? AND nonce=?", node, r.Key).Find(&row)
							if q.Error != nil {
								return q.Error
							}
							if q.RowsAffected == 0 {
								continue
							}
							entry = coldIdentity{Key: row.Nonce, Value: row.ReportID}
						case coldFloor:
							var row model.FlowCursorCheckpoint
							q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id=? AND flow_id=?", node, r.Key).Find(&row)
							if q.Error != nil {
								return q.Error
							}
							if q.RowsAffected == 0 {
								continue
							}
							if row.RawBytes < 0 || row.UploadBytes < 0 || row.DownloadBytes < 0 || row.UsedBytes < 0 {
								return errColdIdentity
							}
							completed := uint64(0)
							if row.IsCompleted {
								completed = 1
							}
							entry = coldIdentity{Key: row.FlowID, Numbers: []uint64{uint64(row.ProtocolCredentialID), row.Revision, uint64(row.RawBytes), uint64(row.UploadBytes), uint64(row.DownloadBytes), uint64(row.UsedBytes), completed}}
						case coldTarget:
							var row model.MeteringLedgerTarget
							q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source_id=?", r.ID).Find(&row)
							if q.Error != nil {
								return q.Error
							}
							if q.RowsAffected == 0 {
								continue
							}
							entry = coldIdentity{Key: strconv.FormatUint(uint64(row.SourceID), 10), Numbers: []uint64{uint64(row.ProtocolEndpointID), uint64(row.RecordID), uint64(row.ResultID)}}
						}
						w, e := coldWeight(tx, kind, entry.Key)
						if e != nil {
							return e
						}
						entry.Weight = w
						byNode[node] = append(byNode[node], entry)
						kept = append(kept, r)
					}
					if e := putColdMany(tx, node, kind, byNode[node]); e != nil {
						return e
					}
					for _, r := range kept {
						var q *gorm.DB
						if kind == coldTarget {
							q = tx.Where("source_id=?", r.ID).Delete(&model.MeteringLedgerTarget{})
						} else {
							q = tx.Table(table).Where("node_id=? AND "+column+"=?", r.NodeID, r.Key).Delete(nil)
						}
						if q.Error != nil {
							return q.Error
						}
						if q.RowsAffected != 1 {
							return fmt.Errorf("cold identity source changed during transfer")
						}
					}
					return nil
				}()
				if nodeErr != nil {
					if !errors.Is(nodeErr, errColdIdentity) && !errors.Is(nodeErr, ErrReportReceiptConflict) {
						return nodeErr
					}
					if e := tx.RollbackTo("cold_node").Error; e != nil {
						return e
					}
					p.HeldCount++
					p.LastHeldNodeID = node
					p.LastHoldReason = "身份段或来源校验失败；原始记录已保留"
				}
			}
			for _, r := range rows {
				p.NodeID, p.AfterKey, p.AfterID = r.NodeID, r.Key, r.ID
			}
			if len(rows) < limit {
				p.NodeID, p.AfterKey, p.AfterID = 0, "", 0
				p.NextPassAt = now.Add(time.Hour)
				if p.HeldCount > 0 {
					notices = append(notices, fmt.Errorf("补报身份 %d 个节点批次暂保留；节点 #%d：%s", p.HeldCount, p.LastHeldNodeID, p.LastHoldReason))
				}
			}
			examined += len(rows)
			return tx.Model(&p).Select("node_id", "after_key", "after_id", "next_pass_at", "held_count", "last_held_node_id", "last_hold_reason").Updates(&p).Error
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			if errors.Is(err, errColdTransferBudget) && limit > 1 {
				n, e := s.Drain(ctx, max(1, limit/2))
				return examined + n, e
			}
			return 0, err
		}
	}
	return examined, errors.Join(notices...)
}
