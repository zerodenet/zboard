package meteringstore

import (
	"errors"
	"fmt"
	"gorm.io/gorm"
)

const coverageRuntimeMaxBytes = 2 * 1024 * 1024
const coverageRuntimeReadBytes = 8 * 1024 * 1024
const coverageRuntimeReadRows = 2048

// Read the bounded set of requested node headers before any full identity.
// Recheck each exact length in the raw query to guard non-snapshot callers too.
func readCoverageRecords(query *gorm.DB) ([]CoverageRecord, error) {
	size := "OCTET_LENGTH(core_instance_id)"
	if query.Dialector.Name() == "sqlite" {
		size = "length(CAST(core_instance_id AS BLOB))"
	}
	var heads []struct {
		NodeID    uint
		BodyBytes int64
	}
	if err := query.Session(&gorm.Session{}).Select("node_id," + size + " AS body_bytes").Limit(coverageRuntimeReadRows + 1).Find(&heads).Error; err != nil {
		return nil, err
	}
	if len(heads) > coverageRuntimeReadRows {
		return nil, errors.New("coverage node scope exceeds bounded read; narrow the scope without inferring zero")
	}
	if len(heads) == 0 {
		return []CoverageRecord{}, nil
	}
	budget := int64(coverageRuntimeReadBytes)
	var filter *gorm.DB
	for _, head := range heads {
		if head.BodyBytes < 0 || head.BodyBytes > coverageRuntimeMaxBytes || head.BodyBytes > budget {
			return nil, errors.New("coverage identity read exceeds evidence budget; coverage remains unknown, narrow the node scope")
		}
		budget -= head.BodyBytes
		condition := "node_id = ? AND " + size + " = ?"
		if filter == nil {
			filter = query.Session(&gorm.Session{NewDB: true}).Where(condition, head.NodeID, head.BodyBytes)
		} else {
			filter = filter.Or(condition, head.NodeID, head.BodyBytes)
		}
	}
	var rows []CoverageRecord
	if err := query.Session(&gorm.Session{}).Where(filter).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) != len(heads) {
		return nil, fmt.Errorf("coverage identity changed during bounded read; retry without inferring zero")
	}
	return rows, nil
}
