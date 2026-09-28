package menustore

import (
	"fmt"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/navigation"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"net/url"
)

// SyncPlugin runs in the caller's installation transaction. Upgrades retain
// operator presentation edits; removed pages/uninstalls delete only owned nodes.
func SyncPlugin(tx *gorm.DB, plugin string, pages []navigation.PageRegistration) error {
	for _, surface := range []string{"public", "account", "admin"} {
		if err := tx.Model(&model.MenuRevision{}).Where("surface = ?", surface).Update("revision", gorm.Expr("revision + 1")).Error; err != nil {
			return err
		}
	}
	keep := []string{}
	for i, p := range pages {
		placement := navigation.Placement{Position: 1000 + i, Icon: "plans"}
		if p.Surface == "admin" {
			placement.ParentID = "settings.extensions"
		}
		if p.Menu != nil {
			placement = *p.Menu
		}
		if !navigation.SurfaceValid(p.Surface) || !placement.Valid() {
			return fmt.Errorf("%w: invalid page registration", navigation.ErrInvalid)
		}
		id := navigation.PluginNodeID(plugin, p.Surface, p.ID)
		var existing int64
		if err := tx.Model(&model.MenuNode{}).Where("id = ?", id).Count(&existing).Error; err != nil {
			return err
		}
		if existing == 0 && placement.ParentID != "" {
			var parent model.MenuNode
			if err := tx.First(&parent, "id = ? AND surface = ? AND path = ?", placement.ParentID, p.Surface, "").Error; err != nil {
				return fmt.Errorf("%w: menu parent does not exist on this surface", navigation.ErrInvalid)
			}
		}
		prefix := ""
		if p.Surface != "public" {
			prefix = "/" + p.Surface
		}
		n := model.MenuNode{ID: id, Surface: p.Surface, ParentID: placement.ParentID, Label: p.Title, Icon: placement.Icon, Position: placement.Position, Hidden: placement.Hidden, Owner: "plugin", PluginID: plugin, PageID: p.ID, Path: prefix + "/extensions/" + url.PathEscape(plugin) + "/" + url.PathEscape(p.ID)}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&n).Error; err != nil {
			return err
		}
		keep = append(keep, n.ID)
	}
	query := tx.Where("owner = ? AND plugin_id = ?", "plugin", plugin)
	if len(keep) > 0 {
		query = query.Where("id NOT IN ?", keep)
	}
	return query.Delete(&model.MenuNode{}).Error
}
