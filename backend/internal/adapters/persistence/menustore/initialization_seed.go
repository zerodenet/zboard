package menustore

import (
	"encoding/json"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MatchesInitializationSeed is read-only recovery evidence for a destination
// claimed while empty. It never makes an arbitrary initialized DB admissible.
// Every persisted node must match the actual embedded built-in node, including
// its label, destination, visibility, placement and access metadata.
func MatchesInitializationSeed(db *gorm.DB) (bool, error) {
	var expected []model.MenuNode
	if err := json.Unmarshal(defaultsJSON, &expected); err != nil {
		return false, err
	}
	var actual []model.MenuNode
	if err := db.Clauses(clause.Locking{Strength: "SHARE"}).Limit(len(expected) + 1).Find(&actual).Error; err != nil {
		return false, err
	}
	if len(actual) != len(expected) {
		return false, nil
	}
	indexed := make(map[string]model.MenuNode, len(expected))
	for _, node := range expected {
		indexed[node.ID] = node
	}
	for _, node := range actual {
		if expected, ok := indexed[node.ID]; !ok || node != expected {
			return false, nil
		}
	}
	var revisions []model.MenuRevision
	if err := db.Clauses(clause.Locking{Strength: "SHARE"}).Limit(4).Find(&revisions).Error; err != nil {
		return false, err
	}
	if len(revisions) != 3 {
		return false, nil
	}
	for _, revision := range revisions {
		switch revision.Surface {
		case "public", "account":
			if revision.Revision != 1 {
				return false, nil
			}
		case "admin":
			// SQLite reports matched rows when convergence rewrites an unchanged
			// placement; MySQL reports changed rows. Both are initial seed outcomes.
			if revision.Revision != 1 && revision.Revision != 2 {
				return false, nil
			}
		default:
			return false, nil
		}
	}
	return true, nil
}
