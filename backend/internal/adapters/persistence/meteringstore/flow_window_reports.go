package meteringstore

import (
	"sort"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/metering"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

// The cumulative cursor normally makes replay of superseded snapshots harmless.
// However, report IDs remain a separate database identity, including MySQL's
// column collation. If a window intersects existing or equivalent report IDs,
// retry its original events through the ordinary bounded accounting path.
func validateFlowWindowReports(tx *gorm.DB, samples []metering.FlowSample) error {
	merged := false
	byNode := make(map[uint][]string)
	for _, sample := range samples {
		if len(sample.Checkpoints) > 0 {
			merged = true
			for _, point := range sample.Checkpoints {
				byNode[sample.NodeID] = append(byNode[sample.NodeID], point.ReportID)
			}
		} else {
			byNode[sample.NodeID] = append(byNode[sample.NodeID], sample.EventID)
		}
	}
	if !merged {
		return nil
	}
	nodes := make([]uint, 0, len(byNode))
	for node := range byNode {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i] < nodes[j] })
	for _, node := range nodes {
		ids := byNode[node]
		intersects := false
		for start := 0; start < len(ids); start += 200 {
			receipts, err := readReportReceipts(tx, node, ids[start:min(start+200, len(ids))])
			if err != nil {
				return err
			}
			if len(receipts) > 0 {
				intersects = true
			}
		}
		if intersects {
			for _, sample := range samples {
				if sample.NodeID != node || len(sample.Checkpoints) == 0 {
					continue
				}
				var cursor model.FlowUsage
				read := tx.Select("revision").Where("node_id = ? AND flow_id = ?", node, metering.FlowUsageKey(sample.CoreInstanceID, sample.FlowID)).Limit(1).Find(&cursor)
				if read.Error != nil {
					return read.Error
				}
				if read.RowsAffected == 0 || cursor.Revision < sample.Sequence {
					return metering.ErrFlowWindowNeedsIndividualReports
				}
			}
		}
		if !datastore.IsSQLite(tx) {
			// The table column lends its collation to the UNION result, even when
			// there are no existing rows. Go equality cannot emulate this contract.
			var query strings.Builder
			query.WriteString("SELECT COUNT(DISTINCT report_id) FROM (SELECT report_id FROM metering_report_receipts WHERE 1 = 0")
			args := make([]any, 0, len(ids))
			for _, id := range ids {
				query.WriteString(" UNION ALL SELECT ?")
				args = append(args, id)
			}
			query.WriteString(") AS window_reports")
			var distinct int64
			if err := tx.Raw(query.String(), args...).Scan(&distinct).Error; err != nil {
				return err
			}
			if distinct != int64(len(ids)) {
				return metering.ErrFlowWindowNeedsIndividualReports
			}
		}
	}
	return nil
}
