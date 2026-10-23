// Package observationstore owns only auxiliary, durable node observations. It
// must never acquire job-ledger, credential, entitlement or metering locks.
package observationstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const PartBytes = 60 * 1024
const MaxApplyBytes int64 = 2 * 1024 * 1024

var ErrMaterial = errors.New("invalid retained observation material")

func invalid(message string) error { return fmt.Errorf("%w: %s", ErrMaterial, message) }

var epoch = time.Unix(0, 0).UTC()

type materialCounter struct {
	io.Writer
	size uint64
}

func (w *materialCounter) Write(b []byte) (int, error) {
	n, e := w.Writer.Write(b)
	w.size += uint64(n)
	return n, e
}

type partWriter struct {
	tx      *gorm.DB
	id      string
	buf     []byte
	ordinal uint64
}

func (w *partWriter) flush() error {
	if len(w.buf) == 0 {
		return nil
	}
	p := model.NodeObservationPart{IntentID: w.id, Ordinal: w.ordinal, Data: w.buf}
	if err := w.tx.Create(&p).Error; err != nil {
		return err
	}
	w.ordinal++
	w.buf = make([]byte, 0, PartBytes)
	return nil
}
func (w *partWriter) Write(b []byte) (n int, err error) {
	for len(b) > 0 {
		take := min(PartBytes-len(w.buf), len(b))
		w.buf = append(w.buf, b[:take]...)
		b = b[take:]
		n += take
		if len(w.buf) == PartBytes {
			if err = w.flush(); err != nil {
				return
			}
		}
	}
	return
}

// Enqueue belongs to the authority transaction. Any header/part failure must
// propagate, rolling back settlement so the spool can replay its exact source.
func Enqueue(tx *gorm.DB, source network.ObservationSource) (string, error) {
	h := sha256.New()
	counter := materialCounter{Writer: h}
	if err := network.WriteObservationSource(&counter, source); err != nil {
		return "", err
	}
	id := hex.EncodeToString(h.Sum(nil))
	now := time.Now().UTC().Truncate(time.Millisecond)
	tag := source.Observation.Latest.AssetTag
	if _, err := hex.DecodeString(tag); len(tag) != 64 || err != nil {
		tag = ""
	} // raw, including malformed labels, remains in parts.
	header := model.NodeObservationIntent{ID: id, NodeID: source.Observation.NodeID, AssetTag: tag, Status: "pending", AvailableAt: now, LeaseUntil: epoch, MaterialBytes: counter.size, PartCount: (counter.size + PartBytes - 1) / PartBytes, CreatedAt: now, UpdatedAt: now}
	if source.LegacyFloor {
		header.Status = "held"
		header.Reason = "unattributed_legacy_floor"
	}
	result := tx.Omit("MaterialRetiredAt", "MaterialReviewAt").Clauses(clause.OnConflict{DoNothing: true}).Create(&header)
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected == 0 {
		var old model.NodeObservationIntent
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&old, "id = ?", id).Error; err != nil {
			return "", err
		}
		if old.MaterialBytes != header.MaterialBytes || old.PartCount != header.PartCount || old.NodeID != header.NodeID || old.AssetTag != header.AssetTag {
			return "", errors.New("observation source header conflict")
		}
		if old.MaterialRetiredAt != nil {
			if err := validateRetiredReceipt(tx, old); err != nil {
				return "", err
			}
			return id, nil
		}
		// Replayed immutable content may already be applied/held. Validate retained
		// parts without resetting disposition, lease, counters or retry state.
		if err := walkParts(tx, old, func(b []byte) error { return nil }); err != nil {
			return "", err
		}
		return id, nil
	}
	w := partWriter{tx: tx, id: id, buf: make([]byte, 0, PartBytes)}
	if err := network.WriteObservationSource(&w, source); err != nil {
		return "", err
	}
	if err := w.flush(); err != nil {
		return "", err
	}
	if w.ordinal != header.PartCount {
		return "", errors.New("observation part count mismatch")
	}
	return id, nil
}

// Each size is checked BEFORE fetching a blob; a forged cached total/ordinal
// cannot trigger a huge driver allocation. No LIMIT alone is a byte budget.
func walkParts(tx *gorm.DB, h model.NodeObservationIntent, accept func([]byte) error) error {
	if h.MaterialRetiredAt != nil {
		return invalid("observation material explicitly retired")
	}

	if h.PartCount == 0 || h.PartCount > (h.MaterialBytes+PartBytes-1)/PartBytes {
		return invalid("invalid observation part count")
	}
	digest := sha256.New()
	var size uint64
	for i := uint64(0); i < h.PartCount; i++ {
		var length int64
		q := tx.Model(&model.NodeObservationPart{}).Select("length(data)").Where("intent_id = ? AND ordinal = ?", h.ID, i).Scan(&length)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 1 || length <= 0 || length > PartBytes {
			return invalid("invalid observation part length")
		}
		// Charge was based on declared total. Reject a forged tiny header
		// BEFORE any blob fetch, not after substr has already spent the budget.
		if size > h.MaterialBytes || uint64(length) > h.MaterialBytes-size {
			return invalid("part exceeds remaining declared material")
		}
		var p model.NodeObservationPart
		if err := tx.Select("intent_id, ordinal, substr(data,1,?) AS data", PartBytes+1).First(&p, "intent_id = ? AND ordinal = ?", h.ID, i).Error; err != nil {
			return err
		}
		if int64(len(p.Data)) != length {
			return invalid("observation part changed")
		}
		size += uint64(len(p.Data))
		if size > h.MaterialBytes {
			return invalid("observation material exceeds declared size")
		}
		_, _ = digest.Write(p.Data)
		if err := accept(p.Data); err != nil {
			return err
		}
	}
	if size != h.MaterialBytes || hex.EncodeToString(digest.Sum(nil)) != h.ID {
		return invalid("observation material digest mismatch")
	}
	var count int64
	if err := tx.Model(&model.NodeObservationPart{}).Where("intent_id = ?", h.ID).Count(&count).Error; err != nil {
		return err
	}
	if uint64(count) != h.PartCount {
		return invalid("unexpected observation material parts")
	}
	return nil
}
func readSource(tx *gorm.DB, h model.NodeObservationIntent) (network.ObservationSource, error) {
	if h.MaterialBytes > uint64(MaxApplyBytes) {
		return network.ObservationSource{}, invalid("observation source exceeds apply byte budget")
	}
	var b bytes.Buffer
	b.Grow(int(h.MaterialBytes))
	if err := walkParts(tx, h, func(p []byte) error { _, err := b.Write(p); return err }); err != nil {
		return network.ObservationSource{}, err
	}
	s, err := network.ReadObservationSource(&b, h.MaterialBytes)
	if err != nil {
		return s, fmt.Errorf("%w: decode: %v", ErrMaterial, err)
	}
	if err == nil && s.Observation.NodeID != h.NodeID {
		err = invalid("observation node identity mismatch")
	}
	return s, err
}
func readReference(tx *gorm.DB, id string) (network.ObservationSource, error) {
	var h model.NodeObservationIntent
	if err := tx.First(&h, "id = ?", id).Error; err != nil {
		return network.ObservationSource{}, err
	}
	return readSource(tx, h)
}

// A compact receipt preserves canonical immutable identity after unreferenced
// auxiliary parts retire. Caller has locked the header and matched the incoming
// complete canonical hash/node/asset/size/count; no freshness or cursor changes.
func validateRetiredReceipt(tx *gorm.DB, h model.NodeObservationIntent) error {
	if h.MaterialRetiredAt == nil || h.Status != "applied" || h.Token != "" || !h.LeaseUntil.Equal(epoch) || h.MaterialBytes == 0 || h.MaterialBytes > uint64(MaxApplyBytes) || h.PartCount != (h.MaterialBytes+PartBytes-1)/PartBytes || h.MaterialRetiredAt.Before(h.CreatedAt) || h.MaterialRetiredAt.Before(h.UpdatedAt) {
		return invalid("invalid compact observation receipt")
	}
	var count int64
	if err := tx.Model(&model.NodeObservationPart{}).Where("intent_id = ?", h.ID).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return invalid("compact receipt still has material parts")
	}
	return nil
}
