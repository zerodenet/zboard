package menustore

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/navigation"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//go:embed defaults.json
var defaultsJSON []byte

// Seed registers missing built-ins once. It never overwrites operator edits.
func Seed(db *gorm.DB) error {
	var defaults []model.MenuNode
	if err := json.Unmarshal(defaultsJSON, &defaults); err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, surface := range []string{"public", "account", "admin"} {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.MenuRevision{Surface: surface, Revision: 1}).Error; err != nil {
				return err
			}
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&defaults).Error
	})
}

func Read(db *gorm.DB, surface string) (navigation.Snapshot, error) {
	var out navigation.Snapshot
	if !navigation.SurfaceValid(surface) {
		return out, fmt.Errorf("%w: unknown surface", navigation.ErrInvalid)
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		var revision model.MenuRevision
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&revision, "surface = ?", surface).Error; err != nil {
			return err
		}
		out.Revision = revision.Revision
		out.Nodes = []navigation.Node{}
		return tx.Where("surface = ?", surface).Order("position, id").Model(&model.MenuNode{}).Scan(&out.Nodes).Error
	})
	return out, err
}

// Replace atomically edits presentation and custom nodes with a surface CAS.
// Built-in/plugin identity, target and access conditions are immutable here.
func Replace(db *gorm.DB, surface string, revision uint64, nodes []navigation.Node, actor string, userID uint) error {
	if !navigation.SurfaceValid(surface) {
		return fmt.Errorf("%w: unknown surface", navigation.ErrInvalid)
	}
	return db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.MenuRevision{}).Where("surface = ? AND revision = ?", surface, revision).Update("revision", gorm.Expr("revision + 1"))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return navigation.ErrConflict
		}
		var previous []model.MenuNode
		if err := tx.Where("surface = ?", surface).Find(&previous).Error; err != nil {
			return err
		}
		old := map[string]model.MenuNode{}
		for _, n := range previous {
			old[n.ID] = n
		}
		for i := range nodes {
			n := &nodes[i]
			p, exists := old[n.ID]
			if exists {
				if n.Surface != p.Surface || n.Owner != p.Owner || n.PluginID != p.PluginID || n.PageID != p.PageID || n.Condition != p.Condition || (p.Owner != "custom" && n.Path != p.Path) {
					return fmt.Errorf("%w: source metadata is immutable", navigation.ErrInvalid)
				}
				delete(old, n.ID)
			} else {
				var count int64
				if err := tx.Model(&model.MenuNode{}).Where("id = ?", n.ID).Count(&count).Error; err != nil {
					return err
				}
				if count != 0 || !strings.HasPrefix(n.ID, "custom:") || n.Owner != "custom" || n.PluginID != "" || n.PageID != "" || n.Condition != "" {
					return fmt.Errorf("%w: invalid custom source", navigation.ErrInvalid)
				}
			}
		}
		for _, p := range old {
			if p.Owner != "custom" {
				return fmt.Errorf("%w: source nodes can be hidden, not deleted", navigation.ErrInvalid)
			}
		}
		if err := navigation.Validate(surface, nodes); err != nil {
			return err
		}
		if err := tx.Where("surface = ?", surface).Delete(&model.MenuNode{}).Error; err != nil {
			return err
		}
		if len(nodes) > 0 {
			rows := make([]model.MenuNode, len(nodes))
			for i, n := range nodes {
				rows[i] = model.MenuNode(n)
			}
			if err := tx.Create(&rows).Error; err != nil {
				return err
			}
		}
		return tx.Create(&model.AuditLog{UserID: &userID, Actor: actor, Action: "navigation.replace", Target: surface, Detail: fmt.Sprintf("%d nodes; revision %d", len(nodes), revision+1)}).Error
	})
}
