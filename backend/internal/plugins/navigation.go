package plugins

import (
	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/menustore"
	"github.com/zerodenet/zboard/backend/internal/navigation"
	"gorm.io/gorm"
)

func registerMenuPages(tx *gorm.DB, manifest Manifest) error {
	pages := []navigation.PageRegistration{}
	for _, p := range manifest.Contributions.Pages {
		if p.Purpose != "configuration" {
			pages = append(pages, navigation.PageRegistration{ID: p.ID, Surface: p.Surface, Title: p.Title, Menu: p.Menu})
		}
	}
	return menustore.SyncPlugin(tx, manifest.ID, pages)
}

// A business page's menu registration also controls whether its UI can open.
// Configuration pages and slots have their own existing access checks.
func (m *Manager) menuPageVisible(plugin, page, surface string) (bool, error) {
	snapshot, err := menustore.Read(m.db, surface)
	if err != nil {
		return false, err
	}
	id := navigation.PluginNodeID(plugin, surface, page)
	for _, node := range navigation.Visible(snapshot.Nodes, func(n navigation.Node) bool { return true }) {
		if node.ID == id {
			return true, nil
		}
	}
	return false, nil
}
