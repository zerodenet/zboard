package navigation_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/menustore"
	"github.com/zerodenet/zboard/backend/internal/datastore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/navigation"
	"gorm.io/gorm"
)

func fixture(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := datastore.OpenWithDriver("sqlite", filepath.Join(t.TempDir(), "menu.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { pool.Close() })
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	return db
}
func read(t *testing.T, db *gorm.DB, surface string) navigation.Snapshot {
	t.Helper()
	s, err := menustore.Read(db, surface)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestMenuEditsSurviveRestartAndConflictWithoutPartialWrite(t *testing.T) {
	db := fixture(t)
	s := read(t, db, "admin")
	s.Nodes[0].Label = "运营入口"
	if err := menustore.Replace(db, "admin", s.Revision, s.Nodes, "operator", 0); err != nil {
		t.Fatal(err)
	}
	if err := datastore.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	retained := read(t, db, "admin")
	if retained.Nodes[0].Label != "运营入口" || retained.Revision != s.Revision+1 {
		t.Fatal("seed overwrote operator edits", retained)
	}
	s.Nodes[0].Label = "过期修改"
	if err := menustore.Replace(db, "admin", s.Revision, s.Nodes, "operator", 0); !errors.Is(err, navigation.ErrConflict) {
		t.Fatal(err)
	}
	if read(t, db, "admin").Nodes[0].Label != "运营入口" {
		t.Fatal("conflict committed")
	}
	var audits int64
	db.Model(&model.AuditLog{}).Where("action = ?", "navigation.replace").Count(&audits)
	if audits != 1 {
		t.Fatal(audits)
	}
}

func TestMenuRejectsForgedSourcesCyclesAndCrossSurfaceParents(t *testing.T) {
	db := fixture(t)
	for _, mutation := range []func(*navigation.Snapshot){
		func(s *navigation.Snapshot) { s.Nodes[0].ParentID = s.Nodes[0].ID },
		func(s *navigation.Snapshot) { s.Nodes[0].ParentID = "public:/" },
		func(s *navigation.Snapshot) { s.Nodes[0].Owner = "custom" },
		func(s *navigation.Snapshot) {
			for i := range s.Nodes {
				if s.Nodes[i].Path != "" {
					s.Nodes[i].Path = "/replacement"
					break
				}
			}
		},
		func(s *navigation.Snapshot) { s.Nodes = s.Nodes[1:] },
		func(s *navigation.Snapshot) {
			s.Nodes = append(s.Nodes, navigation.Node{ID: "custom:malicious", Surface: "admin", Label: "bad", Owner: "custom", Path: "//example.test"})
		},
	} {
		s := read(t, db, "admin")
		mutation(&s)
		if err := menustore.Replace(db, "admin", s.Revision, s.Nodes, "operator", 0); !errors.Is(err, navigation.ErrInvalid) {
			t.Fatal("accepted invalid mutation", err)
		}
		if got := read(t, db, "admin").Revision; got != s.Revision {
			t.Fatal("failed edit changed revision")
		}
	}
}

func TestPluginRegistrationIsPersistentAndPreservesMenuEdits(t *testing.T) {
	db := fixture(t)
	pages := []navigation.PageRegistration{{ID: "home", Surface: "admin", Title: "扩展", Menu: &navigation.Placement{ParentID: "infrastructure.resources", Position: 15, Icon: "nodes"}}}
	if err := db.Transaction(func(tx *gorm.DB) error { return menustore.SyncPlugin(tx, "example.test", pages) }); err != nil {
		t.Fatal(err)
	}
	s := read(t, db, "admin")
	id := navigation.PluginNodeID("example.test", "admin", "home")
	for i := range s.Nodes {
		if s.Nodes[i].ID == id {
			s.Nodes[i].Label = "自定义标题"
			s.Nodes[i].Hidden = true
			s.Nodes[i].ParentID = "customers.service"
		}
	}
	if err := menustore.Replace(db, "admin", s.Revision, s.Nodes, "operator", 0); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return menustore.SyncPlugin(tx, "example.test", pages) }); err != nil {
		t.Fatal(err)
	}
	var node model.MenuNode
	if err := db.First(&node, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	if node.Label != "自定义标题" || !node.Hidden || node.ParentID != "customers.service" {
		t.Fatal("upgrade overwrote menu edit", node)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return menustore.SyncPlugin(tx, "example.test", nil) }); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&node, "id = ?", id).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("uninstall retained menu", err)
	}
	if len(read(t, db, "admin").Nodes) == 0 {
		t.Fatal("removed core nodes")
	}
}

func TestVisiblePrunesHiddenAncestorsAndEmptyGroups(t *testing.T) {
	nodes := []navigation.Node{{ID: "hidden", Hidden: true}, {ID: "child", ParentID: "hidden", Path: "/child"}, {ID: "empty"}, {ID: "plugin-group"}, {ID: "plugin", ParentID: "plugin-group", Path: "/plugin", Owner: "plugin"}, {ID: "visible", Path: "/visible"}}
	out := navigation.Visible(nodes, func(n navigation.Node) bool { return n.Owner != "plugin" })
	if len(out) != 1 || out[0].ID != "visible" {
		t.Fatal(out)
	}
}
