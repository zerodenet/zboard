package meteringstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const flowStatisticsLimit = 200
const flowStatisticsSourceColumns = "id,node_id,subscription_id,occurred_at,received_at,created_at,statistics_registered"

func flowStatisticsDigest(r FlowStartRecord) string {
	raw, _ := json.Marshal(struct {
		Version                     int
		ID                          uint64
		Node, Subscription          uint
		Occurred, Received, Created string
	}{1, r.ID, r.NodeID, r.SubscriptionID, r.OccurredAt.UTC().Format(time.RFC3339Nano), r.ReceivedAt.UTC().Format(time.RFC3339Nano), r.CreatedAt.UTC().Format(time.RFC3339Nano)})
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

// Read the ACTUAL persisted representation (including MySQLdatetime(3)), not
// the pre-insert nanosecond value. Current read remains correct under old RR.
func enqueueFlowStatistics(tx *gorm.DB, node uint, event string) error {
	var r FlowStartRecord
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select(flowStatisticsSourceColumns).Where("node_id=? AND event_id=?", node, event).Take(&r).Error; err != nil {
		return err
	}
	if r.ID == 0 || r.ReceivedAt.IsZero() || r.OccurredAt.IsZero() || r.CreatedAt.IsZero() {
		return errors.New("flow statistics source identity/time missing")
	}
	pending := model.FlowStartStatisticsPending{SourceID: r.ID, NodeID: r.NodeID, SubscriptionID: r.SubscriptionID, OccurredAt: r.OccurredAt, ReceivedAt: r.ReceivedAt, CreatedAt: time.Now().UTC()}
	// Raw -> pending -> point matches projector/retainer order; no RowsAffected
	// freshness inference can bypass identity checks with CLIENT_FOUND_ROWS.
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&pending).Error; err != nil {
		return err
	}
	var actual model.FlowStartStatisticsPending
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&actual, "source_id=?", r.ID).Error; err != nil {
		return err
	}
	if actual.NodeID != r.NodeID || actual.SubscriptionID != r.SubscriptionID || !actual.OccurredAt.Equal(r.OccurredAt) || !actual.ReceivedAt.Equal(r.ReceivedAt) {
		return errors.New("flow statistics pending identity changed")
	}
	var point model.FlowStartStatisticsPoint
	q := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("source_id=?", r.ID).Limit(1).Find(&point)
	if q.Error != nil {
		return q.Error
	}
	if q.RowsAffected > 0 {
		if !flowStatisticsPointMatches(point, r) {
			return errors.New("flow statistics receipt conflicts with original source")
		}
		if err := tx.Where("source_id=?", r.ID).Delete(&model.FlowStartStatisticsPending{}).Error; err != nil {
			return err
		}
		return markFlowStatisticsRegistered(tx, r.ID)
	}
	return markFlowStatisticsRegistered(tx, r.ID)
}
func flowStatisticsPointMatches(p model.FlowStartStatisticsPoint, r FlowStartRecord) bool {
	return p.SourceID == r.ID && p.NodeID == r.NodeID && p.SubscriptionID == r.SubscriptionID && p.OccurredAt.Equal(r.OccurredAt) && p.ReceivedAt.Equal(r.ReceivedAt) && p.SourceCreatedAt.Equal(r.CreatedAt) && p.FactSHA256 == flowStatisticsDigest(r)
}

func markFlowStatisticsRegistered(tx *gorm.DB, id uint64) error {
	return tx.Model(&FlowStartRecord{}).Where("id=? AND statistics_registered=?", id, false).UpdateColumn("statistics_registered", true).Error
}
