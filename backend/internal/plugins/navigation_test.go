package plugins

import (
	"context"
	"errors"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/adapters/persistence/menustore"
	"github.com/zerodenet/zboard/backend/internal/model"
	"github.com/zerodenet/zboard/backend/internal/navigation"
)

func TestPluginLifecycleRegistersPagesAndRemovesOnlyOwnedMenus(t *testing.T) {
	raw, keys := fixturePackage(t, nil)
	m, db, _ := testManager(t, keys)
	v, err := importFixture(t, m, raw)
	if err != nil {
		t.Fatal(err)
	}
	var nodes []model.MenuNode
	if err := db.Where("plugin_id = ?", v.ID).Find(&nodes).Error; err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatal("business pages did not enter database, or config page became menu", nodes)
	}
	if pages, err := m.Pages("public", 0, false); err != nil || len(pages) != 0 {
		t.Fatal("disabled plugin visible", pages, err)
	}
	v, err = m.Action(context.Background(), v.ID, "enable", "admin", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if pages, err := m.Pages("public", 0, false); err != nil || len(pages) != 1 {
		t.Fatal("enabled plugin not visible", pages, err)
	}
	v, err = m.Action(context.Background(), v.ID, "disable", "admin", v.Generation, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if pages, err := m.Pages("public", 0, false); err != nil || len(pages) != 0 {
		t.Fatal("disabled plugin still visible", pages, err)
	}
	if _, err = m.Action(context.Background(), v.ID, "uninstall", "admin", v.Generation, false, ""); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&model.MenuNode{}).Where("plugin_id = ?", v.ID).Count(&count)
	if count != 0 {
		t.Fatal("uninstalled menus remain")
	}
	s, err := menustore.Read(db, "public")
	if err != nil || len(s.Nodes) == 0 {
		t.Fatal("core menu damaged", err)
	}
}

func TestHiddenPluginPageOrParentBlocksNewAndExistingUISessions(t *testing.T) {
	for _, parentHidden := range []bool{false, true} {
		t.Run(map[bool]string{false: "page", true: "parent"}[parentHidden], func(t *testing.T) {
			raw, keys := fixturePackage(t, nil)
			m, db, _ := testManager(t, keys)
			v, err := importFixture(t, m, raw)
			if err != nil {
				t.Fatal(err)
			}
			v, err = m.Action(context.Background(), v.ID, "enable", "admin", v.Generation, false, "")
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := menustore.Read(db, "public")
			if err != nil {
				t.Fatal(err)
			}
			id := navigation.PluginNodeID(v.ID, "public", "home")
			if parentHidden {
				snapshot.Nodes = append(snapshot.Nodes, navigation.Node{ID: "custom:folder", Surface: "public", Owner: "custom", Label: "分组"})
				for i := range snapshot.Nodes {
					if snapshot.Nodes[i].ID == id {
						snapshot.Nodes[i].ParentID = "custom:folder"
					}
				}
				if err := menustore.Replace(db, "public", snapshot.Revision, snapshot.Nodes, "admin", 0); err != nil {
					t.Fatal(err)
				}
				snapshot, err = menustore.Read(db, "public")
				if err != nil {
					t.Fatal(err)
				}
				id = "custom:folder"
			}
			session, err := m.CreateSession(v.ID, "home", "public", 0, false, false)
			if err != nil {
				t.Fatal(err)
			}
			for i := range snapshot.Nodes {
				if snapshot.Nodes[i].ID == id {
					snapshot.Nodes[i].Hidden = true
				}
			}
			if err := menustore.Replace(db, "public", snapshot.Revision, snapshot.Nodes, "admin", 0); err != nil {
				t.Fatal(err)
			}
			if _, err := m.CreateSession(v.ID, "home", "public", 0, false, false); !errors.Is(err, ErrPermission) {
				t.Fatal("hidden page opened a new session", err)
			}
			if _, err := m.CheckSession(session.Token, 0, false); !errors.Is(err, ErrInvalidSession) {
				t.Fatal("hidden page retained usable session", err)
			}
			if _, err := m.Asset(session.Token, "ui/index.html"); !errors.Is(err, ErrInvalidSession) {
				t.Fatal("hidden page asset still served", err)
			}
		})
	}
}

func TestPluginRejectsMenuPlacementAcrossSurfaces(t *testing.T) {
	raw, keys := fixturePackage(t, func(m *Manifest, _ map[string][]byte) {
		m.Contributions.Pages[0].Menu = &navigation.Placement{ParentID: "settings.extensions"}
	})
	m, db, _ := testManager(t, keys)
	if _, err := importFixture(t, m, raw); err == nil {
		t.Fatal("public page mounted below admin menu")
	}
	var count int64
	db.Model(&model.PluginInstallation{}).Count(&count)
	if count != 0 {
		t.Fatal("rejected registration partially installed plugin")
	}
}
