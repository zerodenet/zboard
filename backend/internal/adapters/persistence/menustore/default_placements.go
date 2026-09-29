package menustore

import (
	_ "embed"
	"encoding/json"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

//go:embed legacy_placements.json
var legacyPlacementsJSON []byte

// Evolve untouched defaults only. Stable IDs keep plugin placements and route
// bindings intact; any operator presentation edit exempts that node entirely.
func convergeDefaultPlacements(tx *gorm.DB, defaults []model.MenuNode) error {
	var legacy []model.MenuNode
	if err := json.Unmarshal(legacyPlacementsJSON, &legacy); err != nil {
		return err
	}
	current := make(map[string]model.MenuNode, len(defaults))
	for _, node := range defaults {
		current[node.ID] = node
	}
	changed := false
	for _, previous := range legacy {
		next := current[previous.ID]
		res := tx.Model(&model.MenuNode{}).
			Where("id = ? AND surface = ? AND owner = ? AND label = ? AND parent_id = ? AND position = ? AND icon = ? AND hidden = ? AND path = ? AND `condition` = ?", previous.ID, previous.Surface, previous.Owner, previous.Label, previous.ParentID, previous.Position, previous.Icon, previous.Hidden, previous.Path, previous.Condition).
			Updates(map[string]any{"label": next.Label, "parent_id": next.ParentID, "position": next.Position})
		if res.Error != nil {
			return res.Error
		}
		changed = changed || res.RowsAffected > 0
	}
	if changed {
		return tx.Model(&model.MenuRevision{}).Where("surface = ?", "admin").Update("revision", gorm.Expr("revision + 1")).Error
	}
	return nil
}
