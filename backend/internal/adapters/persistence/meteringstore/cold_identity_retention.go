package meteringstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

type coldCandidate struct {
	NodeID uint
	Key    string
	ID     uint
}
type coldSource struct {
	candidate coldCandidate
	entry     coldIdentity
}
type coldMaintenancePlan struct {
	progress   model.MeteringColdProgress
	rows       []coldCandidate
	sources    []coldSource
	pages      *coldPreparedPages
	complete   bool
	due        bool
	prepareErr error
}

func coldTable(kind uint8) (string, string) {
	switch kind {
	case coldReport:
		return "metering_report_receipts", "report_id"
	case coldNonce:
		return "metering_nonce_receipts", "nonce"
	case coldFloor:
		return "flow_cursor_checkpoints", "flow_id"
	default:
		return "metering_ledger_targets", "source_id"
	}
}

// sourceColdIdentity is deliberately narrow: counters/identity are captured and
// compared again under the node/source fence, not inferred from today's quota.
func sourceColdIdentity(tx *gorm.DB, kind uint8, r coldCandidate, now time.Time, lock bool) (coldIdentity, bool, error) {
	var entry coldIdentity
	q := tx.Session(&gorm.Session{})
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	switch kind {
	case coldReport:
		var row model.MeteringReportReceipt
		v := q.Where("node_id=? AND report_id=?", r.NodeID, r.Key).Find(&row)
		if v.Error != nil {
			return entry, false, v.Error
		}
		if v.RowsAffected == 0 || row.CreatedAt.After(now.Add(-LedgerMinuteHorizon)) {
			return entry, false, nil
		}
		var link model.MeteringLedgerTarget
		ok, e := loadLedgerTarget(tx, r.NodeID, row.TrafficRecordID, &link)
		if e != nil || !ok {
			return entry, false, e
		}
		entry = coldIdentity{Key: row.ReportID, Numbers: []uint64{uint64(row.ProtocolEndpointID), uint64(row.TrafficRecordID)}}
	case coldNonce:
		var row model.MeteringNonceReceipt
		v := q.Where("node_id=? AND nonce=?", r.NodeID, r.Key).Find(&row)
		if v.Error != nil || v.RowsAffected == 0 {
			return entry, false, v.Error
		}
		entry = coldIdentity{Key: row.Nonce, Value: row.ReportID}
	case coldFloor:
		var row model.FlowCursorCheckpoint
		v := q.Where("node_id=? AND flow_id=?", r.NodeID, r.Key).Find(&row)
		if v.Error != nil || v.RowsAffected == 0 {
			return entry, false, v.Error
		}
		if row.RawBytes < 0 || row.UploadBytes < 0 || row.DownloadBytes < 0 || row.UsedBytes < 0 {
			return entry, false, errColdIdentity
		}
		completed := uint64(0)
		if row.IsCompleted {
			completed = 1
		}
		entry = coldIdentity{Key: row.FlowID, Numbers: []uint64{uint64(row.ProtocolCredentialID), row.Revision, uint64(row.RawBytes), uint64(row.UploadBytes), uint64(row.DownloadBytes), uint64(row.UsedBytes), completed}}
	case coldTarget:
		var row model.MeteringLedgerTarget
		v := q.Where("source_id=?", r.ID).Find(&row)
		if v.Error != nil || v.RowsAffected == 0 {
			return entry, false, v.Error
		}
		entry = coldIdentity{Key: strconv.FormatUint(uint64(row.SourceID), 10), Numbers: []uint64{uint64(row.ProtocolEndpointID), uint64(row.RecordID), uint64(row.ResultID)}}
	default:
		return entry, false, errColdIdentity
	}
	return entry, true, nil
}

func (s ColdIdentities) prepare(ctx context.Context, kind uint8, limit int, now time.Time) (p coldMaintenancePlan, err error) {
	// Read one consistent snapshot, retaining no row locks. CPU-heavy merge and
	// gzip happen only AFTER this snapshot transaction has closed.
	var incoming []coldIdentity
	var targets []uint
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		p.progress = model.MeteringColdProgress{Kind: kind, NextPassAt: time.Unix(0, 0).UTC()}
		q := tx.Where("kind=?", kind).Find(&p.progress)
		if q.Error != nil {
			return q.Error
		}
		if p.progress.NextPassAt.After(now) {
			return nil
		}
		p.due = true
		table, column := coldTable(kind)
		var rows []coldCandidate
		if kind == coldTarget {
			q = tx.Table(table).Select("source_id AS id,node_id").Where("source_id>?", p.progress.AfterID).Order("source_id")
		} else {
			q = tx.Table(table).Select("node_id,"+column+" AS `key`").Where("node_id>? OR (node_id=? AND "+column+">?)", p.progress.NodeID, p.progress.NodeID, p.progress.AfterKey).Order("node_id," + column)
		}
		if e := q.Limit(limit).Find(&rows).Error; e != nil {
			return e
		}
		// Each commit owns only one node. No previously processed node remains
		// locked while waiting for the next node or doing its compression work.
		p.rows = rows
		for i := range rows {
			if rows[i].NodeID != rows[0].NodeID {
				p.rows = rows[:i]
				break
			}
		}
		p.complete = len(rows) < limit && len(rows) == len(p.rows)
		if len(p.rows) == 0 {
			return nil
		}
		keys := make([]string, 0, len(p.rows))
		for _, r := range p.rows {
			keys = append(keys, r.Key)
		}
		if e := prepareColdWeights(tx, kind, keys); e != nil {
			return e
		}
		for _, r := range p.rows {
			entry, ok, e := sourceColdIdentity(tx, kind, r, now, false)
			if e != nil {
				return e
			}
			if !ok {
				continue
			}
			entry.Weight, e = coldWeight(tx, kind, entry.Key)
			if e != nil {
				return e
			}
			incoming = append(incoming, entry)
			p.sources = append(p.sources, coldSource{r, entry})
		}
		if len(incoming) > 0 {
			var e error
			p.pages, targets, e = snapshotColdPages(tx, p.rows[0].NodeID, kind, incoming)
			return e
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err == nil && len(p.rows) > 0 {
		err = EnsureIdentityFences(s.DB.WithContext(ctx), []uint{p.rows[0].NodeID})
	}
	if err == nil && p.pages != nil {
		err = p.pages.prepare(p.rows[0].NodeID, kind, incoming, targets)
	}
	// Corrupt evidence is held, never discarded or reconstructed. Its original
	// sources remain untouched, and later nodes can still make progress.
	if errors.Is(err, errColdIdentity) || errors.Is(err, ErrReportReceiptConflict) {
		p.prepareErr = err
		err = nil
	}
	return
}

func (s ColdIdentities) commit(ctx context.Context, kind uint8, now time.Time, p coldMaintenancePlan) (notice error, err error) {
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current := model.MeteringColdProgress{Kind: kind, NextPassAt: time.Unix(0, 0).UTC()}
		if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&current).Error; e != nil {
			return e
		}
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "kind=?", kind).Error; e != nil {
			return e
		}
		if !sameColdProgress(current, p.progress) {
			return errColdPlanStale
		}
		if current.NodeID == 0 && current.AfterID == 0 && current.AfterKey == "" {
			current.HeldCount, current.LastHeldNodeID, current.LastHoldReason = 0, 0, ""
		}
		if len(p.rows) > 0 {
			node := p.rows[0].NodeID
			// An exclusive identity fence cannot block node heartbeat, grants or
			// payments. Compression has already finished outside this transaction.
			if tx.Dialector.Name() == "mysql" {
				var fence model.MeteringIdentityFence
				if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id=?", node).First(&fence).Error; e != nil {
					return e
				}
			}
			if e := tx.SavePoint("cold_node").Error; e != nil {
				return e
			}
			nodeErr := p.prepareErr
			if nodeErr == nil {
				for _, source := range p.sources {
					actual, ok, e := sourceColdIdentity(tx, kind, source.candidate, now, true)
					if e != nil {
						return e
					}
					actual.Weight = source.entry.Weight
					if !ok || !reflect.DeepEqual(actual, source.entry) {
						return errColdPlanStale
					}
				}
				if p.pages != nil {
					nodeErr = p.pages.apply(tx, node, kind)
				}
				if nodeErr == nil {
					table, column := coldTable(kind)
					for _, source := range p.sources {
						q := tx.Table(table)
						if kind == coldTarget {
							q = q.Where("source_id=?", source.candidate.ID)
						} else {
							q = q.Where("node_id=? AND "+column+"=?", node, source.candidate.Key)
						}
						deleted := q.Delete(nil)
						if deleted.Error != nil {
							return deleted.Error
						}
						if deleted.RowsAffected != 1 {
							return errColdPlanStale
						}
					}
				}
			}
			if nodeErr != nil {
				if !errors.Is(nodeErr, errColdIdentity) && !errors.Is(nodeErr, ErrReportReceiptConflict) {
					return nodeErr
				}
				if e := tx.RollbackTo("cold_node").Error; e != nil {
					return e
				}
				current.HeldCount++
				current.LastHeldNodeID = node
				current.LastHoldReason = "身份段或来源校验失败；原始记录已保留"
			}
			r := p.rows[len(p.rows)-1]
			current.NodeID, current.AfterKey, current.AfterID = r.NodeID, r.Key, r.ID
		}
		if p.complete {
			current.NodeID, current.AfterKey, current.AfterID = 0, "", 0
			current.NextPassAt = now.Add(time.Hour)
			if current.HeldCount > 0 {
				notice = fmt.Errorf("补报身份 %d 个节点批次暂保留；节点 #%d：%s", current.HeldCount, current.LastHeldNodeID, current.LastHoldReason)
			}
		}
		return tx.Model(&current).Select("node_id", "after_key", "after_id", "next_pass_at", "held_count", "last_held_node_id", "last_hold_reason").Updates(&current).Error
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	return
}

// Each kind examines at most limit sources, committing separately per node.
// Preparation and compression hold no accounting/source locks. A stale plan
// leaves both identities and checkpoint intact for the next scheduled attempt.
func (s ColdIdentities) Drain(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 200 {
		return 0, errors.New("cold identity batch accepts 1..200 sources")
	}
	examined := 0
	var notices []error
	now := s.now().Truncate(time.Millisecond)
	for kind := uint8(1); kind <= 4; kind++ {
		remaining := limit
		for remaining > 0 {
			p, e := s.prepare(ctx, kind, remaining, now)
			if errors.Is(e, errColdTransferBudget) && remaining > 1 {
				n, e := s.Drain(ctx, max(1, remaining/2))
				return examined + n, e
			}
			if e != nil {
				return examined, e
			}
			if !p.due {
				break
			}
			notice, e := s.commit(ctx, kind, now, p)
			if errors.Is(e, errColdPlanStale) {
				break
			}
			if e != nil {
				return examined, e
			}
			if notice != nil {
				notices = append(notices, notice)
			}
			examined += len(p.rows)
			remaining -= len(p.rows)
			if p.complete || len(p.rows) == 0 {
				break
			}
		}
	}
	return examined, errors.Join(notices...)
}

func sameColdProgress(a, b model.MeteringColdProgress) bool {
	return a.Kind == b.Kind && a.NodeID == b.NodeID && a.AfterID == b.AfterID && a.AfterKey == b.AfterKey && a.NextPassAt.Equal(b.NextPassAt) && a.HeldCount == b.HeldCount && a.LastHeldNodeID == b.LastHeldNodeID && a.LastHoldReason == b.LastHoldReason
}
