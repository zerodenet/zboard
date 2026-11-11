package menustore

import (
	_ "embed"
	"encoding/json"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
)

//go:embed legacy_placements.json
var legacyPlacementsJSON []byte

//go:embed workspace_legacy_defaults.json
var workspaceLegacyDefaultsJSON []byte

// Evolve untouched defaults only. Stable IDs keep plugin placements and route
// bindings intact; any operator presentation edit exempts that node entirely.
func convergeDefaultPlacements(tx *gorm.DB, defaults []model.MenuNode, changed map[string]bool) error {
	var legacy []model.MenuNode
	if err := json.Unmarshal(legacyPlacementsJSON, &legacy); err != nil {
		return err
	}
	var workspaceLegacy []model.MenuNode
	if err := json.Unmarshal(workspaceLegacyDefaultsJSON, &workspaceLegacy); err != nil {
		return err
	}
	legacy = append(legacy, workspaceLegacy...)
	current := make(map[string]model.MenuNode, len(defaults))
	for _, node := range defaults {
		current[node.ID] = node
	}
	for _, previous := range legacy {
		next, exists := current[previous.ID]
		if !exists || (previous.Label == next.Label && previous.ParentID == next.ParentID && previous.Position == next.Position) {
			continue
		}
		res := tx.Model(&model.MenuNode{}).
			Where("id = ? AND surface = ? AND owner = ? AND label = ? AND parent_id = ? AND position = ? AND icon = ? AND hidden = ? AND path = ? AND `condition` = ?", previous.ID, previous.Surface, previous.Owner, previous.Label, previous.ParentID, previous.Position, previous.Icon, previous.Hidden, previous.Path, previous.Condition).
			Updates(map[string]any{"label": next.Label, "parent_id": next.ParentID, "position": next.Position})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected > 0 {
			changed[previous.Surface] = true
		}
	}
	for _, surface := range []string{"admin", "account", "public"} {
		if changed[surface] {
			if err := tx.Model(&model.MenuRevision{}).Where("surface = ?", surface).Update("revision", gorm.Expr("revision + 1")).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
