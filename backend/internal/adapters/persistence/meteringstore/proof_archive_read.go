package meteringstore

import (
	"errors"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// These bounds apply only to explicitly requested full diagnostic/proof reads.
// Core summaries and quota reads never enter this path. A too-large legacy
// unpaged request fails explicitly before allocating an unbounded record slice.
const fullProofReadRows = 2000
const fullProofReadOutputBytes uint64 = 8 * 1024 * 1024
const fullProofReadMaterialBytes = 16 * 1024 * 1024

func proofReadBudgetError() error {
	return errors.Join(metering.ErrProofReadBudget, &metering.PolicyValidation{Fields: map[string]string{"query": "完整计量证明超过单次2000行、8MiB输出或16MiB材料预算，请分页或缩小查询范围。"}})
}

// Inspect only a bounded selected page of IDs/lengths before reading Meta. The
// surrounding repeatable-read snapshot binds this discovery to actual loading.
func preflightFullProofRead(tx, selected *gorm.DB) (int, error) {
	var rows []struct {
		ID        uint
		EventType string
		MetaBytes uint64
	}
	if err := selected.Session(&gorm.Session{}).Select("id,event_type,COALESCE(octet_length(meta),0) AS meta_bytes").Find(&rows).Error; err != nil {
		return 0, err
	}
	if len(rows) > fullProofReadRows {
		return 0, proofReadBudgetError()
	}
	ids := make([]uint, 0, len(rows))
	for _, r := range rows {
		if r.EventType != "" {
			ids = append(ids, r.ID)
		}
	}
	headers := map[uint]model.TrafficMetaArchive{}
	for start := 0; start < len(ids); start += 200 {
		var batch []model.TrafficMetaArchive
		if err := tx.Model(&model.TrafficMetaArchive{}).Select("traffic_record_id,version,source_bytes,compressed_bytes").Where("traffic_record_id IN ?", ids[start:min(start+200, len(ids))]).Find(&batch).Error; err != nil {
			return 0, errors.Join(metering.ErrProofArchive, err)
		}
		for _, h := range batch {
			if h.Version != 1 || h.SourceBytes == 0 || h.SourceBytes > proofArchiveBodyLimit || h.CompressedBytes == 0 || h.CompressedBytes > proofArchiveBodyLimit {
				return 0, metering.ErrProofArchive
			}
			headers[h.TrafficRecordID] = h
		}
	}
	var output, live, material uint64
	for _, r := range rows {
		if r.MetaBytes > fullProofReadOutputBytes {
			return 0, proofReadBudgetError()
		}
		live += r.MetaBytes
		material += r.MetaBytes
		if h, ok := headers[r.ID]; ok {
			output += h.SourceBytes
			material += h.SourceBytes + h.CompressedBytes + 2
		} else {
			output += r.MetaBytes
		}
		if output > fullProofReadOutputBytes || material > fullProofReadMaterialBytes {
			return 0, proofReadBudgetError()
		}
	}
	return fullProofReadMaterialBytes - int(live), nil
}
