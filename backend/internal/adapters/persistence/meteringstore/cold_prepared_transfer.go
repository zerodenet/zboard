package meteringstore

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errColdPlanStale = errors.New("cold identity preparation changed; retry without moving checkpoint")

type coldPreparedPages struct {
	root    model.MeteringColdRoot
	old     map[uint]*coldTransferPage
	writes  []model.MeteringColdBlock
	deletes []uint
}

// Read/decode a consistent snapshot without retaining accounting/node locks.
func snapshotColdPages(tx *gorm.DB, node uint, kind uint8, in []coldIdentity) (*coldPreparedPages, []uint, error) {
	pages, targets, e := coldTransferPages(tx, node, kind, in)
	if e != nil {
		return nil, nil, e
	}
	p := &coldPreparedPages{old: pages}
	if e := tx.Where("node_id=? AND kind=?", node, kind).Find(&p.root).Error; e != nil {
		return nil, nil, e
	}
	return p, targets, nil
}

// Merge, split, encode and round-trip check before entering the write transaction.
func (p *coldPreparedPages) prepare(node uint, kind uint8, in []coldIdentity, targets []uint) error {
	merged := map[uint][]coldIdentity{}
	for id, page := range p.old {
		merged[id] = append([]coldIdentity(nil), page.entries...)
	}
	for i, entry := range in {
		same := false
		for _, old := range merged[targets[i]] {
			if bytes.Equal(old.Weight, entry.Weight) {
				if !reflect.DeepEqual(old, entry) {
					return ErrReportReceiptConflict
				}
				same = true
				break
			}
		}
		if !same {
			merged[targets[i]] = append(merged[targets[i]], entry)
		}
	}
	ids := make([]uint, 0, len(p.old))
	for id := range p.old {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		b := p.old[id].b
		if e := p.page(node, kind, b.Depth, b.Prefix, id, merged[id]); e != nil {
			return e
		}
	}
	return nil
}
func (p *coldPreparedPages) page(node uint, kind, depth uint8, prefix uint32, id uint, entries []coldIdentity) error {
	raw, e := coldRaw(entries)
	if e != nil {
		return e
	}
	if len(entries) > coldBlockEntries || len(raw) > coldBlockBytes {
		if depth == 32 {
			return errColdIdentity
		}
		if id != 0 {
			p.deletes = append(p.deletes, id)
		}
		left, right := []coldIdentity{}, []coldIdentity{}
		for _, r := range entries {
			if coldPrefix(coldRoute(r.Weight), depth+1) == prefix*2 {
				left = append(left, r)
			} else {
				right = append(right, r)
			}
		}
		if e := p.page(node, kind, depth+1, prefix*2, 0, left); e != nil {
			return e
		}
		return p.page(node, kind, depth+1, prefix*2+1, 0, right)
	}
	b, e := encodeCold(entries)
	if e != nil {
		return e
	}
	b.ID, b.NodeID, b.Kind, b.Depth, b.Prefix = id, node, kind, depth, prefix
	b.HeaderSHA256 = coldHeaderDigest(b)
	got, e := decodeCold(b)
	if e != nil || !reflect.DeepEqual(got, entries) {
		return errColdIdentity
	}
	p.writes = append(p.writes, b)
	return nil
}

// Caller owns the exclusive node fence. Validate every observed input BEFORE
// modifying any page. A concurrent floor reopening/split causes a fresh prepare,
// never a lost update or an identity admitted from a stale negative lookup.
func (p *coldPreparedPages) apply(tx *gorm.DB, node uint, kind uint8) error {
	var root model.MeteringColdRoot
	if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id=? AND kind=?", node, kind).Find(&root).Error; e != nil {
		return e
	}
	if root.NodeID != p.root.NodeID || root.Kind != p.root.Kind {
		return errColdPlanStale
	}
	ids := make([]uint, 0, len(p.old))
	for id := range p.old {
		if id != 0 {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	var oldCount int64
	for _, id := range ids {
		var b model.MeteringColdBlock
		q := tx.Select(coldHeaderSelect+",substr(payload,1,?) AS payload", coldBlockBytes+1025).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", id).Find(&b)
		if q.Error != nil {
			return q.Error
		}
		old := p.old[id].b
		if q.RowsAffected != 1 || b.HeaderSHA256 != old.HeaderSHA256 || !validColdHeader(b) || !bytes.Equal(b.Payload, old.Payload) {
			return errColdPlanStale
		}
		oldCount += int64(old.EntryCount)
	}
	if root.PageCount < int64(len(ids)) || root.EntryCount < oldCount {
		return errColdIdentity
	}
	for _, id := range p.deletes {
		if e := tx.Delete(&model.MeteringColdBlock{}, id).Error; e != nil {
			return e
		}
	}
	var newCount int64
	for _, prepared := range p.writes {
		b := prepared
		var e error
		if b.ID == 0 {
			e = tx.Create(&b).Error
		} else {
			e = tx.Model(&b).Select("version", "entry_count", "raw_bytes", "sha256", "payload", "bloom", "header_sha256").Updates(&b).Error
		}
		if e != nil {
			return e
		}
		// Verify exact stored bytes/metadata without decompressing under the fence.
		var stored model.MeteringColdBlock
		if e := tx.Select(coldHeaderSelect+",substr(payload,1,?) AS payload", coldBlockBytes+1025).First(&stored, b.ID).Error; e != nil {
			return e
		}
		if !validColdHeader(stored) || stored.HeaderSHA256 != b.HeaderSHA256 || !bytes.Equal(stored.Payload, b.Payload) {
			return errColdIdentity
		}
		newCount += int64(b.EntryCount)
	}
	tx.Statement.Settings.Delete(fmt.Sprintf("metering:cold-headers:%d:%d", node, kind))
	return adjustColdRoot(tx, node, kind, int64(len(p.writes)-len(ids)), newCount-oldCount)
}
