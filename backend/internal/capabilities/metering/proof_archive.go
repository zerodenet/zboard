package metering

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

var ErrProofArchive = errors.New("metering proof archive is unavailable or inconsistent; restore verified evidence")
var ErrProofReadBudget = errors.New("full metering evidence exceeds its read budget; paginate or narrow the history range")

// ProofFactDigest binds the complete accounting identity, including fields
// intentionally hidden from public JSON. Both archive reads and downgrade
// preparation use this same definition; material storage is the only omission.
func ProofFactDigest(record TrafficRecord) string {
	record.Meta = ""
	// Existing proof digests predate minute settlement. A legacy count of one
	// is implicit and must not change an already sealed financial identity.
	if !record.IsAggregate {
		record.RecordCount = 0
	}
	record.At = record.At.UTC()
	record.CreatedAt = record.CreatedAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()
	raw, _ := json.Marshal(struct {
		Record TrafficRecord
		Nonce  string
		Epoch  uint64
	}{record, record.Nonce, record.EntitlementEpoch})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

type ProofArchiveRequest struct {
	AfterID   uint
	ThroughID uint
	Before    time.Time
	Limit     int
}

type ProofArchiveResult struct {
	Scanned              int    `json:"scanned"`
	Archived             int    `json:"archived"`
	AlreadySealed        int    `json:"already_sealed"`
	Held                 int    `json:"held"`
	NoSaving             int    `json:"no_saving"`
	NextAfterID          uint   `json:"next_after_id"`
	RawBytes             uint64 `json:"raw_bytes"`
	EstimatedStoredBytes uint64 `json:"estimated_stored_bytes"`
	RangeExhausted       bool   `json:"range_exhausted"`
}

type ProofRestoreResult struct {
	TrafficRecordID uint `json:"traffic_record_id"`
	Restored        bool `json:"restored"`
}

type ProofArchiveRepository interface {
	Archive(context.Context, ProofArchiveRequest) (ProofArchiveResult, error)
	Restore(context.Context, uint) (ProofRestoreResult, error)
}

type ProofArchives struct{ Repository ProofArchiveRepository }

func (s ProofArchives) Archive(ctx context.Context, q ProofArchiveRequest) (ProofArchiveResult, error) {
	if ctx == nil || q.Limit < 1 || q.Limit > 32 || q.ThroughID == 0 || q.AfterID > q.ThroughID || q.Before.IsZero() {
		return ProofArchiveResult{}, errors.New("archive requires an explicit ID range, time boundary and batch of 1..32")
	}
	if err := ctx.Err(); err != nil {
		return ProofArchiveResult{}, err
	}
	return s.Repository.Archive(ctx, q)
}

func (s ProofArchives) Restore(ctx context.Context, id uint) (ProofRestoreResult, error) {
	if ctx == nil || id == 0 {
		return ProofRestoreResult{}, errors.New("proof restoration requires one explicit ledger ID")
	}
	if err := ctx.Err(); err != nil {
		return ProofRestoreResult{}, err
	}
	return s.Repository.Restore(ctx, id)
}
